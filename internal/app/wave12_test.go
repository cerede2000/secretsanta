package app

import (
	"net/http"
	"net/url"
	"strings"
	"testing"
)

// Les pages de la vague 12 (ajustement, réglages, relances) sont montées
// derrière la session administrateur, et le lien du diagnostic de délivrabilité
// atterrit sur sa page.
func TestWave12AdminPagesMounted(t *testing.T) {
	e := newW9(t)
	anon := newBrowser(t, e.a)
	for _, p := range []string{"/noel/admin/ajuster", "/noel/admin/reglages", "/noel/admin/relances"} {
		if rec := anon.get(p); rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/noel/admin/connexion" {
			t.Errorf("%s sans session : %d %q", p, rec.Code, rec.Header().Get("Location"))
		}
		if rec := e.admin.get(p); rec.Code != http.StatusOK {
			t.Errorf("%s : %d", p, rec.Code)
		}
	}
	body := e.admin.get("/noel/admin/reglages").Body.String()
	if !strings.Contains(body, `href="/noel/admin/courrier/delivrabilite"`) {
		t.Error("lien du diagnostic de délivrabilité absent des réglages")
	}
	if rec := e.admin.get("/noel/admin/courrier/delivrabilite"); rec.Code != http.StatusOK {
		t.Errorf("page de délivrabilité : %d", rec.Code)
	}
	// Sans transport, la relance manuelle est refusée proprement (pas de 500).
	rel := "/noel/admin/relances"
	if rec := e.admin.post(rel, rel, url.Values{"type": {"lien"}, "person": {"1"}}); rec.Code >= 500 {
		t.Errorf("relance sans transport : %d", rec.Code)
	}
}
