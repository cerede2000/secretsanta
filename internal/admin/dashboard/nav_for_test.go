package dashboard

import (
	"net/http"
	"strings"
	"testing"

	"secretsanta/internal/shared/adminnav"
)

// NavFor voit la requête : le slug du chemin remplit les destinations du
// registre, et la variante l'emporte sur Nav.
func TestNavForSeesRequestAndWinsOverNav(t *testing.T) {
	e := newEnv(t)
	gid := e.group("noel")
	e.pages.o.Nav = func(string) []adminnav.Item {
		return []adminnav.Item{{Entry: adminnav.Entry{Key: "x", Label: "layout.nav.dashboard", Icon: "dashboard", Destination: "/ancienne"}}}
	}
	e.pages.o.NavFor = func(r *http.Request, _ string) []adminnav.Item {
		return []adminnav.Item{{Entry: adminnav.Entry{Key: "x", Label: "layout.nav.dashboard", Icon: "dashboard",
			Destination: "/" + r.PathValue("slug") + "/admin"}}}
	}
	rec := e.get("/noel/admin", "fr", e.adminCookie(gid))
	if rec.Code != http.StatusOK {
		t.Fatalf("statut %d", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, `href="/noel/admin"`) || strings.Contains(body, "/ancienne") {
		t.Fatalf("navigation inattendue :\n%s", body)
	}
}
