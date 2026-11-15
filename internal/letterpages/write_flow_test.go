package letterpages

import (
	"net/http"
	"net/url"
	"strings"
	"testing"

	"secretsanta/internal/participants"
)

func TestWriteFlowAddImageDeleteAndErrors(t *testing.T) {
	e := newEnv(t)
	a := e.add(t, "Alice", participants.RoleNormal)
	b := e.add(t, "Bobby", participants.RoleNormal)
	c := e.login(t, a)

	// Message enregistré, avant tout tirage.
	rec := e.do(t, http.MethodPost, "/noel/lettre", reqOpt{cookie: c, form: url.Values{"intro": {"Bonjour"}}})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("intro : %d", rec.Code)
	}
	// Idée avec photo.
	rec = e.do(t, http.MethodPost, "/noel/lettre/idees", reqOpt{cookie: c, multipart: map[string]string{"title": "Mug", "url": "https://exemple.fr/mug"}, image: pngBytes(t)})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("idée : %d %s", rec.Code, rec.Body.String())
	}
	l, _ := e.letters.Get(ctx, e.gid, a)
	if len(l.Ideas) != 1 || !l.Ideas[0].HasImage {
		t.Fatalf("idée absente : %+v", l)
	}
	page := e.get(t, "/noel/lettre", c).Body.String()
	img := "/noel/lettre/image/" + itoa(l.Ideas[0].ID)
	if !strings.Contains(page, img) || !strings.Contains(page, "exemple.fr/mug") {
		t.Error("idée ou image non affichée")
	}
	if r := e.get(t, img, c); r.Code != 200 || r.Header().Get("Content-Type") != "image/png" {
		t.Errorf("image propre : %d %s", r.Code, r.Header().Get("Content-Type"))
	}
	// L'image n'est pas servie à une autre personne.
	if r := e.get(t, img, e.login(t, b)); r.Code != http.StatusNotFound {
		t.Errorf("image d'un autre : %d", r.Code)
	}
	// Erreur sous le champ.
	rec = e.do(t, http.MethodPost, "/noel/lettre/idees", reqOpt{cookie: c, multipart: map[string]string{"title": "", "text": "x"}})
	body := rec.Body.String()
	if rec.Code != http.StatusUnprocessableEntity || !strings.Contains(body, `aria-invalid="true"`) || !strings.Contains(body, `role="alert"`) {
		t.Errorf("erreur non intégrée au champ : %d", rec.Code)
	}
	// Plafond de 5 idées.
	for i := 0; i < 4; i++ {
		e.do(t, http.MethodPost, "/noel/lettre/idees", reqOpt{cookie: c, multipart: map[string]string{"title": "Idée"}})
	}
	rec = e.do(t, http.MethodPost, "/noel/lettre/idees", reqOpt{cookie: c, multipart: map[string]string{"title": "Sixième"}})
	if rec.Code != http.StatusUnprocessableEntity {
		t.Errorf("6e idée : %d", rec.Code)
	}
	// Suppression de sa propre idée ; pas celle d'un autre.
	id := l.Ideas[0].ID
	if r := e.do(t, http.MethodPost, "/noel/lettre/idees/"+itoa(id)+"/supprimer", reqOpt{cookie: e.login(t, b)}); r.Code != http.StatusNotFound {
		t.Errorf("suppression par un autre : %d", r.Code)
	}
	if r := e.do(t, http.MethodPost, "/noel/lettre/idees/"+itoa(id)+"/supprimer", reqOpt{cookie: c}); r.Code != http.StatusSeeOther {
		t.Errorf("suppression : %d", r.Code)
	}
	if l, _ = e.letters.Get(ctx, e.gid, a); len(l.Ideas) != 4 {
		t.Errorf("idées restantes : %d", len(l.Ideas))
	}
}
