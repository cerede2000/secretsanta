package accesspages

import (
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"secretsanta/internal/websec"
)

func adminForm(secret string) url.Values { return url.Values{"secret": {secret}} }

func TestAdminLoginOpensSessionForGoodSecret(t *testing.T) {
	e := newEnv(t)
	g := e.createGroup(t, "noel")
	rec := e.do(t, http.MethodPost, "/noel/admin/connexion", adminForm(g.AdminSecret), "198.51.100.7", "fr")
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/noel/admin" {
		t.Fatalf("redirection attendue : %d %q", rec.Code, rec.Header().Get("Location"))
	}
	if len(e.sessions.opened) != 1 {
		t.Fatalf("une session attendue, %d", len(e.sessions.opened))
	}
	id := e.sessions.opened[0]
	if id.Kind != websec.KindAdmin || id.GroupID == "" || id.GroupID == "0" {
		t.Fatalf("identité : %+v", id)
	}
}

func TestAdminLoginRefusalsAreUniform(t *testing.T) {
	e := newEnv(t)
	g1 := e.createGroup(t, "noel")
	e.createGroup(t, "autre")
	cases := map[string]struct{ path, secret string }{
		"mauvais secret":           {"/noel/admin/connexion", "faux"},
		"secret vide":              {"/noel/admin/connexion", ""},
		"secret d'un autre groupe": {"/autre/admin/connexion", g1.AdminSecret},
		"groupe inconnu":           {"/inconnu/admin/connexion", g1.AdminSecret},
	}
	var first string
	for name, c := range cases {
		rec := e.do(t, http.MethodPost, c.path, adminForm(c.secret), "198.51.100.8", "fr")
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("%s : statut %d", name, rec.Code)
		}
		body := csrfRe.ReplaceAllString(rec.Body.String(), "")
		// Le chemin du formulaire contient le slug : on le neutralise.
		body = strings.NewReplacer("noel", "SLUG", "autre", "SLUG", "inconnu", "SLUG").Replace(body)
		if first == "" {
			first = body
		} else if body != first {
			t.Fatalf("%s : réponse différente des autres refus", name)
		}
		if rec.Header().Get("Set-Cookie") != "" && strings.Contains(rec.Header().Get("Set-Cookie"), "test_admin") {
			t.Fatalf("%s : session ouverte", name)
		}
	}
	if e.sessions.count() != 0 {
		t.Fatal("aucune session ne devait s'ouvrir")
	}
}

func TestAdminLoginBlocksRepeatedAttempts(t *testing.T) {
	e := newEnv(t)
	g := e.createGroup(t, "noel")
	for i := 0; i < AdminAttempts.Limit; i++ {
		if rec := e.do(t, http.MethodPost, "/noel/admin/connexion", adminForm("faux"), "198.51.100.9", "fr"); rec.Code != http.StatusUnauthorized {
			t.Fatalf("essai %d : %d", i, rec.Code)
		}
	}
	// Même le bon secret est refusé dans la fenêtre.
	if rec := e.do(t, http.MethodPost, "/noel/admin/connexion", adminForm(g.AdminSecret), "198.51.100.9", "fr"); rec.Code != http.StatusTooManyRequests {
		t.Fatalf("429 attendu, %d", rec.Code)
	}
	if e.sessions.count() != 0 {
		t.Fatal("aucune session pendant le blocage")
	}
	e.clock.Advance(AdminAttempts.Window + time.Second)
	if rec := e.do(t, http.MethodPost, "/noel/admin/connexion", adminForm(g.AdminSecret), "198.51.100.9", "fr"); rec.Code != http.StatusSeeOther {
		t.Fatalf("la fenêtre écoulée doit rouvrir l'accès : %d", rec.Code)
	}
}

func TestAdminPageShowsFormAndHidesUnknownGroup(t *testing.T) {
	e := newEnv(t)
	e.createGroup(t, "noel")
	rec := e.do(t, http.MethodGet, "/noel/admin/connexion", nil, "198.51.100.1", "fr")
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), `name="secret"`) || !strings.Contains(rec.Body.String(), "Noël en famille") {
		t.Fatalf("page : %d", rec.Code)
	}
	if rec := e.do(t, http.MethodGet, "/inconnu/admin/connexion", nil, "198.51.100.1", "fr"); rec.Code != 404 {
		t.Fatalf("404 attendu : %d", rec.Code)
	}
}
