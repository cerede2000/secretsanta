package letterpages

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"secretsanta/internal/participants"
)

func TestZZSessionRefusals(t *testing.T) {
	e := newEnv(t)
	e.add(t, "Alice", participants.RoleNormal)
	if r := e.get(t, "/inconnu/lettre", nil); r.Code != http.StatusNotFound {
		t.Errorf("groupe inconnu : %d", r.Code)
	}
	for _, p := range []string{"/noel/lettre", "/noel/lettre/image/1", "/noel/lettre-recue/1"} {
		if r := e.get(t, p, nil); r.Code != http.StatusSeeOther {
			t.Errorf("%s sans session : %d", p, r.Code)
		}
	}
	// Sujet de session nul.
	if r := e.get(t, "/noel/lettre", e.login(t, 0)); r.Code != http.StatusSeeOther {
		t.Errorf("sujet nul : %d", r.Code)
	}
}

func TestZZWriteValidationAndIdentifiers(t *testing.T) {
	e := newEnv(t)
	a := e.add(t, "Alice", participants.RoleNormal)
	c := e.login(t, a)

	long := strings.Repeat("x", 5000)
	if r := e.do(t, http.MethodPost, "/noel/lettre", reqOpt{cookie: c, form: url.Values{"intro": {long}}}); r.Code != http.StatusUnprocessableEntity {
		t.Errorf("intro trop longue : %d", r.Code)
	}
	if r := e.do(t, http.MethodPost, "/noel/lettre/idees/abc/supprimer", reqOpt{cookie: c}); r.Code != http.StatusNotFound {
		t.Errorf("identifiant invalide : %d", r.Code)
	}
	if r := e.do(t, http.MethodPost, "/noel/lettre/idees/999/supprimer", reqOpt{cookie: c}); r.Code != http.StatusNotFound {
		t.Errorf("idée inconnue : %d", r.Code)
	}
	if r := e.get(t, "/noel/lettre/image/abc", c); r.Code != http.StatusNotFound {
		t.Errorf("image invalide : %d", r.Code)
	}
	if r := e.get(t, "/noel/lettre/image/999", c); r.Code != http.StatusNotFound {
		t.Errorf("image inconnue : %d", r.Code)
	}
	// Idée sans image : pas d'image servie.
	if _, err := e.letters.AddIdea(ctx, e.gid, a, ideaIn("Sans photo")); err != nil {
		t.Fatal(err)
	}
	l, _ := e.letters.Get(ctx, e.gid, a)
	if r := e.get(t, "/noel/lettre/image/"+itoa(l.Ideas[0].ID), c); r.Code != http.StatusNotFound {
		t.Errorf("idée sans image : %d", r.Code)
	}
	// Image refusée par le service (pas une image).
	r := e.do(t, http.MethodPost, "/noel/lettre/idees", reqOpt{cookie: c, multipart: map[string]string{"title": "Faux"}, image: []byte("pas une image")})
	if r.Code != http.StatusUnprocessableEntity {
		t.Errorf("fausse image : %d", r.Code)
	}
	// Formulaire non multipart accepté pour l'ajout d'idée.
	if r := e.do(t, http.MethodPost, "/noel/lettre/idees", reqOpt{cookie: c, form: url.Values{"title": {"Sans fichier"}}}); r.Code != http.StatusSeeOther {
		t.Errorf("idée sans multipart : %d", r.Code)
	}
}

func TestZZReceivedRefusals(t *testing.T) {
	e := newEnv(t)
	a := e.add(t, "Alice", participants.RoleNormal)
	for _, n := range []string{"Bobby", "Carol", "Denis"} {
		e.add(t, n, participants.RoleNormal)
	}
	c := e.login(t, a)
	for _, p := range []string{"/noel/lettre-recue/abc", "/noel/lettre-recue/0", "/noel/lettre-recue/9999"} {
		if r := e.get(t, p, c); r.Code != http.StatusNotFound {
			t.Errorf("%s : %d", p, r.Code)
		}
	}
}

func TestZZInternalFailures(t *testing.T) {
	e := newEnv(t)
	a := e.add(t, "Alice", participants.RoleNormal)
	c := e.login(t, a)
	if _, err := e.db.Exec(`DROP TABLE letter_ideas`); err != nil {
		t.Fatal(err)
	}
	if _, err := e.db.Exec(`DROP TABLE letters`); err != nil {
		// Les clés étrangères peuvent s'y opposer : on tente l'inverse.
		t.Fatal(err)
	}
	for name, r := range map[string]*httptest.ResponseRecorder{
		"page":   e.get(t, "/noel/lettre", c),
		"intro":  e.do(t, http.MethodPost, "/noel/lettre", reqOpt{cookie: c, form: url.Values{"intro": {"x"}}}),
		"idee":   e.do(t, http.MethodPost, "/noel/lettre/idees", reqOpt{cookie: c, multipart: map[string]string{"title": "x"}}),
		"efface": e.do(t, http.MethodPost, "/noel/lettre/idees/1/supprimer", reqOpt{cookie: c}),
		"limage": e.get(t, "/noel/lettre/image/1", c),
		"recue":  e.get(t, "/noel/lettre-recue/1", c),
	} {
		if r.Code < 400 {
			t.Errorf("%s : %d", name, r.Code)
		}
	}
}
