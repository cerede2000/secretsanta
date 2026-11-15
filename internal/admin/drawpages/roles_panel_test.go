package drawpages

import (
	"strings"
	"testing"

	"secretsanta/internal/participants"
)

// F10.AC26 (part de cette page) : le panneau « Rôles dans le tirage » de la page
// du tirage donne le récapitulatif en comptes, sans aucun nom, renvoie au panneau
// de modification en lot (F10.6) et, après le tirage, ne propose aucun nouveau
// tirage silencieux : un changement de rôle ne crée aucun tirage.
func TestRolesPanelRecapInCountsWithoutNames(t *testing.T) {
	e := newEnv(t)
	gid := e.group("roles")
	e.addN(gid, "Hortensia", 3, participants.RoleNormal)
	e.addN(gid, "Plus", 2, participants.RoleOffrePlus)
	e.addN(gid, "Seul", 1, participants.RoleUnSeul)
	e.addN(gid, "Mamie", 2, participants.RoleReceptionSeule)
	ck := e.adminCookie(gid)

	body := e.get("/roles/admin/tirage", "fr", ck).Body.String()
	if !strings.Contains(body, `id="drw-roles"`) {
		t.Fatal("panneau des rôles absent")
	}
	for _, want := range []string{"Réception seule", "Un seul cadeau", "Offre des cadeaux en plus", "Capacité totale", "/roles/admin/roles"} {
		if !strings.Contains(body, want) {
			t.Errorf("panneau sans %q", want)
		}
	}
	// Les comptes : 3 normal, 2 offre plus, 1 un seul, 2 réception seule.
	section := body[strings.Index(body, `id="drw-roles"`):]
	section = section[:strings.Index(section, "</section>")]
	for _, want := range []string{"<dd>3</dd>", "<dd>2</dd>", "<dd>1</dd>"} {
		if !strings.Contains(section, want) {
			t.Errorf("récapitulatif sans %s", want)
		}
	}
	e.noNames(t, "récapitulatif", body)

	// Changer un rôle (comme le fait le panneau en lot) met le récapitulatif à jour.
	list, _ := e.parts.List(ctx, participants.AdminActor(gid), gid)
	for _, p := range list {
		if p.Role == participants.RoleReceptionSeule {
			if err := e.parts.SetRole(ctx, participants.AdminActor(gid), gid, p.ID, participants.RoleNormal); err != nil {
				t.Fatal(err)
			}
		}
	}
	if after := e.get("/roles/admin/tirage", "fr", ck).Body.String(); after == body {
		t.Error("le récapitulatif doit suivre les rôles")
	}

	// Après le tirage : le récapitulatif reste en comptes, rien ne relance le tirage.
	e.doDraw(gid)
	n := e.drawCount(gid)
	post := e.get("/roles/admin/tirage", "fr", ck)
	if post.Code != 200 {
		t.Fatalf("après tirage : %d", post.Code)
	}
	e.noNames(t, "après tirage", post.Body.String())
	if e.drawCount(gid) != n {
		t.Fatal("afficher la page ne doit jamais tirer")
	}
	if !strings.Contains(post.Body.String(), "/roles/admin/roles") {
		t.Error("après le tirage, le panneau de modification doit rester atteignable")
	}
}
