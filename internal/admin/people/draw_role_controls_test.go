package people

import (
	"net/url"
	"regexp"
	"strings"
	"testing"

	"secretsanta/internal/participants"
	"secretsanta/internal/shared/ports"
)

func TestListAndSheetOfferTheFourExclusiveRolesBadgeAndFilter(t *testing.T) {
	e := newEnv(t)
	a := e.person(t, e.groupA, "Alice", "alice@exemple.fr", participants.RoleNormal)
	b := e.person(t, e.groupA, "Bruno", "bruno@exemple.fr", participants.RoleUnSeul)
	c := e.person(t, e.groupA, "Chloe", "chloe@exemple.fr", participants.RoleReceptionSeule)
	ck := e.adminCookies(t, e.groupA)

	body := e.get(t, "/noel/admin/participants", "fr", ck).Body.String()
	if n := strings.Count(body, `action="/noel/admin/roles"`); n != 3 {
		t.Errorf("un sélecteur de rôle par personne attendu, %d", n)
	}
	for _, v := range []string{"normal", "reception_seule", "offre_plus", "un_seul"} {
		if strings.Count(body, `<option value="`+v+`"`) < 3 {
			t.Errorf("valeur %q absente des sélecteurs", v)
		}
	}
	if !strings.Contains(body, "roles-badge-un_seul") || !strings.Contains(body, "roles-badge-reception_seule") {
		t.Error("badges de rôle absents")
	}
	if strings.Contains(body, "roles-badge-normal") {
		t.Error("aucune pastille pour le rôle normal")
	}
	if !strings.Contains(body, "Un seul cadeau") || !strings.Contains(body, "Reçoit seulement") {
		t.Error("libellés des rôles absents")
	}

	only := func(q string) []string {
		b := e.get(t, "/noel/admin/participants"+q, "fr", ck).Body.String()
		return regexp.MustCompile(`class="ppl-name" href="/noel/admin/participants/(\d+)"`).FindAllString(b, -1)
	}
	if got := only("?role=un_seul"); len(got) != 1 || !strings.Contains(got[0], "/"+sid(b)+`"`) {
		t.Errorf("filtre un_seul : %v", got)
	}
	if got := only("?role=reception_seule"); len(got) != 1 || !strings.Contains(got[0], "/"+sid(c)+`"`) {
		t.Errorf("filtre reception_seule : %v", got)
	}
	if got := only("?role=normal"); len(got) != 1 || !strings.Contains(got[0], "/"+sid(a)+`"`) {
		t.Errorf("filtre normal : %v", got)
	}

	sheet := e.get(t, "/noel/admin/participants/"+sid(b), "fr", ck).Body.String()
	if !strings.Contains(sheet, `action="/noel/admin/roles"`) || !strings.Contains(sheet, "roles-badge-un_seul") ||
		!strings.Contains(sheet, `name="next" value="/noel/admin/participants/`+sid(b)+`"`) {
		t.Error("sélecteur ou badge absent de la fiche")
	}
}

func TestRoleSelectorIsAdminOnlyAndBeforeDrawIsDirect(t *testing.T) {
	e := newEnv(t)
	a := e.person(t, e.groupA, "Alice", "alice@exemple.fr", participants.RoleNormal)
	if rec := e.get(t, "/noel/admin/participants", "fr", nil); rec.Code != 303 {
		t.Errorf("sans session : 303 attendu, %d", rec.Code)
	}
	if rec := e.get(t, "/noel/admin/participants", "fr", e.adminCookies(t, e.groupB)); rec.Code != 303 {
		t.Errorf("session d'un autre groupe : 303 attendu, %d", rec.Code)
	}
	ck := e.adminCookies(t, e.groupA)
	rec := e.post(t, "/noel/admin/roles", url.Values{"ids": {sid(a)}, "role": {"offre_plus"}, "next": {"/noel/admin/participants"}}, "fr", ck)
	if rec.Code != 303 || rec.Header().Get("Location") != "/noel/admin/participants" {
		t.Fatalf("retour à la liste attendu : %d %s", rec.Code, rec.Header().Get("Location"))
	}
	var role string
	if err := e.db.QueryRow(`SELECT draw_role FROM participants WHERE id = ?`, a).Scan(&role); err != nil || role != "offre_plus" {
		t.Errorf("rôle : %q %v", role, err)
	}
}

func TestRoleChangeAfterDrawGoesThroughF3dWithCountsOnlyPreview(t *testing.T) {
	e := newEnv(t)
	a := e.person(t, e.groupA, "Alice", "alice@exemple.fr", participants.RoleNormal)
	b := e.person(t, e.groupA, "Bruno", "bruno@exemple.fr", participants.RoleNormal)
	c := e.person(t, e.groupA, "Chloe", "chloe@exemple.fr", participants.RoleNormal)
	e.seedDraw(t, e.groupA, []int64{a, b, c})
	e.repair.Previews = map[ports.RuleKind]ports.RepairPreview{ports.RuleRoleChange: {AffectedCount: 1, AlreadyDiscoveredCount: 1, Repairable: true}}
	ck := e.adminCookies(t, e.groupA)
	rec := e.post(t, "/noel/admin/roles", url.Values{"ids": {sid(a)}, "role": {"un_seul"}, "next": {"/noel/admin/participants"}}, "fr", ck)
	body := rec.Body.String()
	if rec.Code != 200 || !strings.Contains(body, "Personnes touchées : 1") {
		t.Fatalf("aperçu en comptes attendu : %d", rec.Code)
	}
	// L'aperçu ne nomme personne autre que les lignes de la liste des rôles ;
	// il ne contient aucun indice de cible.
	for _, w := range []string{"→", "offre à Bruno", "offre à Chloe", "cible de"} {
		if strings.Contains(body, w) {
			t.Errorf("indice de paire %q dans l'aperçu", w)
		}
	}
	var role string
	if err := e.db.QueryRow(`SELECT draw_role FROM participants WHERE id = ?`, a).Scan(&role); err != nil || role != "normal" {
		t.Errorf("rien ne doit être écrit avant la confirmation : %q", role)
	}
	// La liste des participants rappelle le passage par l'aperçu après le tirage.
	if !strings.Contains(e.get(t, "/noel/admin/participants", "fr", ck).Body.String(), "Jamais de nouveau tirage silencieux") {
		t.Error("rappel de l'aperçu après tirage absent")
	}
}
