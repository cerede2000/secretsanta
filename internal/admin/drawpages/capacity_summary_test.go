package drawpages

import (
	"net/http"
	"net/url"
	"strings"
	"testing"

	"secretsanta/internal/participants"
)

// F10.AC27 : capacité insuffisante, explication et propositions en comptes, sans
// nom, lancement bloqué.
func TestInsufficientCapacityExplainsAndBlocksLaunch(t *testing.T) {
	e := newEnv(t)
	gid := e.group("capacite")
	e.addN(gid, "Hortensia", 4, participants.RoleUnSeul)
	e.addN(gid, "Mamie", 3, participants.RoleReceptionSeule)
	ck := e.adminCookie(gid)

	for _, lang := range []string{"fr", "en"} {
		body := e.get("/capacite/admin/tirage", lang, ck).Body.String()
		if !strings.Contains(body, `role="alert"`) {
			t.Fatalf("%s : l'explication doit être une alerte", lang)
		}
		if !strings.Contains(body, `id="drw-cap"`) {
			t.Errorf("%s : section de capacité absente", lang)
		}
		if strings.Contains(body, "tirage?confirmer=lancer") {
			t.Errorf("%s : le lancement ne doit pas être proposé", lang)
		}
		if !strings.Contains(body, "/capacite/admin/roles") {
			t.Errorf("%s : lien vers le panneau des rôles absent", lang)
		}
		e.noNames(t, "capacité "+lang, body)
	}
	fr := e.get("/capacite/admin/tirage", "fr", ck).Body.String()
	if !strings.Contains(fr, "Propositions") || !strings.Contains(fr, "Il manque") {
		t.Errorf("explication et propositions attendues : %s", fr)
	}
	// Même en forçant la requête, aucun tirage n'est créé.
	rec := e.post("/capacite/admin/tirage/lancer", "/capacite/admin/tirage?confirmer=lancer", ck, url.Values{"confirm": {"1"}})
	if rec.Code == http.StatusSeeOther || e.drawCount(gid) != 0 {
		t.Fatalf("le lancement doit être refusé : %d, tirages %d", rec.Code, e.drawCount(gid))
	}
	e.noNames(t, "refus", rec.Body.String())

	// Une fois la capacité rétablie (un rôle changé), le lancement est proposé.
	list, err := e.parts.List(ctx, participants.AdminActor(gid), gid)
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range list {
		if p.Role == participants.RoleUnSeul {
			if err := e.parts.SetRole(ctx, participants.AdminActor(gid), gid, p.ID, participants.RoleNormal); err != nil {
				t.Fatalf("SetRole : %v", err)
			}
		}
	}
	if body := e.get("/capacite/admin/tirage", "fr", ck).Body.String(); !strings.Contains(body, "tirage?confirmer=lancer") || strings.Contains(body, `id="drw-cap"`) {
		t.Error("capacité suffisante : le lancement doit être proposé sans alerte")
	}
}
