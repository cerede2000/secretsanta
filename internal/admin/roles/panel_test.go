package roles

import (
	"net/http"
	"net/url"
	"strings"
	"testing"

	"secretsanta/internal/participants"
	"secretsanta/internal/shared/ports"
)

func TestPanelShowsCountsAndNoPair(t *testing.T) {
	e := newEnv(t)
	ck := e.adminCookies(t, e.groupA)
	e.person(t, e.groupA, "Alice", participants.RoleNormal)
	e.person(t, e.groupA, "Bruno", participants.RoleNormal)
	e.person(t, e.groupA, "Chloe", participants.RoleNormal)
	e.person(t, e.groupA, "David", participants.RoleNormal)
	e.person(t, e.groupA, "Emma", participants.RoleReceptionSeule)
	e.person(t, e.groupA, "Farid", participants.RoleReceptionSeule)
	rec := e.get(t, "/noel/admin/roles", "fr", ck)
	if rec.Code != 200 {
		t.Fatalf("code %d", rec.Code)
	}
	body := rec.Body.String()
	for _, want := range []string{"Rôles dans le tirage", "Suffisant", "Cadeaux supplémentaires nécessaires",
		"Capacité totale", "Modifier en lot", "Alice"} {
		if !strings.Contains(body, want) {
			t.Errorf("%q absent", want)
		}
	}
	if strings.Contains(body, `role="alert"`) {
		t.Error("alerte inattendue sur un état suffisant")
	}
	// quatre valeurs exclusives dans chaque sélecteur
	if n := strings.Count(body, `value="reception_seule"`); n < 6 {
		t.Errorf("sélecteurs incomplets : %d", n)
	}
}

func TestPanelInsufficientCapacityExplainsAndProposes(t *testing.T) {
	e := newEnv(t)
	ck := e.adminCookies(t, e.groupA)
	for _, n := range []string{"Alice", "Bruno", "Chloe", "David"} {
		e.person(t, e.groupA, n, participants.RoleUnSeul)
	}
	e.person(t, e.groupA, "Emma", participants.RoleReceptionSeule)
	e.person(t, e.groupA, "Farid", participants.RoleReceptionSeule)
	body := e.get(t, "/noel/admin/roles", "fr", ck).Body.String()
	for _, want := range []string{"Insuffisant", "Trop de personnes ne peuvent pas ou ne veulent pas offrir plus d&#39;un cadeau : il manque 2 cadeaux.", "Propositions", "/noel/admin/reglages"} {
		if !strings.Contains(body, want) {
			t.Errorf("%q absent", want)
		}
	}
}

func TestPanelRequiresAdminSessionOfThatGroup(t *testing.T) {
	e := newEnv(t)
	e.person(t, e.groupA, "Alice", participants.RoleNormal)
	for name, ck := range map[string][]*http.Cookie{"sans session": nil, "autre groupe": e.adminCookies(t, e.groupB)} {
		rec := e.get(t, "/noel/admin/roles", "fr", ck)
		if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/noel/admin/connexion" {
			t.Errorf("%s : %d %q", name, rec.Code, rec.Header().Get("Location"))
		}
		if strings.Contains(rec.Body.String(), "Alice") {
			t.Errorf("%s : fuite de nom", name)
		}
	}
	if rec := e.get(t, "/inconnu/admin/roles", "fr", nil); rec.Code != 404 {
		t.Errorf("groupe inconnu : %d", rec.Code)
	}
}

func TestBatchBeforeDrawWritesDirectly(t *testing.T) {
	e := newEnv(t)
	ck := e.adminCookies(t, e.groupA)
	a := e.person(t, e.groupA, "Alice", participants.RoleNormal)
	b := e.person(t, e.groupA, "Bruno", participants.RoleNormal)
	c := e.person(t, e.groupA, "Chloe", participants.RoleNormal)
	rec := e.post(t, "noel", url.Values{"ids": ids(a, b), "role": {"offre_plus"}}, "fr", ck)
	if rec.Code != http.StatusSeeOther || !strings.Contains(rec.Header().Get("Location"), "applied=2") {
		t.Fatalf("%d %q", rec.Code, rec.Header().Get("Location"))
	}
	if e.roleOf(t, a) != "offre_plus" || e.roleOf(t, b) != "offre_plus" || e.roleOf(t, c) != "normal" {
		t.Fatal("rôles inattendus")
	}
	if len(e.journal) != 1 || e.journal[0].counts["changes"] != 2 {
		t.Fatalf("journal : %+v", e.journal)
	}
	if e.repair.RepairCalls != 0 {
		t.Fatal("aucune réparation avant le tirage")
	}
	// la page de retour annonce le compte
	body := e.get(t, rec.Header().Get("Location"), "fr", ck).Body.String()
	if !strings.Contains(body, "2 rôles modifiés.") {
		t.Error("notice absente")
	}
}

func TestBatchAfterDrawOnePreviewThenRepair(t *testing.T) {
	e := newEnv(t)
	ck := e.adminCookies(t, e.groupA)
	a := e.person(t, e.groupA, "Alice", participants.RoleNormal)
	b := e.person(t, e.groupA, "Bruno", participants.RoleNormal)
	e.state.Done[e.groupA] = true
	e.repair.Previews = map[ports.RuleKind]ports.RepairPreview{
		ports.RuleRoleChange: {AffectedCount: 2, AlreadyDiscoveredCount: 1, Repairable: true},
	}
	rec := e.post(t, "noel", url.Values{"ids": ids(a, b), "role": {"reception_seule"}}, "fr", ck)
	if rec.Code != 200 {
		t.Fatalf("%d", rec.Code)
	}
	body := rec.Body.String()
	for _, want := range []string{"Changements concernés : 2", "Personnes touchées : 4", "Dont ont déjà découvert leur cible : 2", "Réparer le tirage", "Garder pour les futurs tirages"} {
		if !strings.Contains(body, want) {
			t.Errorf("%q absent", want)
		}
	}
	if e.roleOf(t, a) != "normal" || e.roleOf(t, b) != "normal" || e.repair.RepairCalls != 0 {
		t.Fatal("rien ne doit être écrit avant la confirmation")
	}
	rec = e.post(t, "noel", url.Values{"ids": ids(a, b), "role": {"reception_seule"}, "choice": {"repair"}}, "fr", ck)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("confirmation : %d", rec.Code)
	}
	if e.roleOf(t, a) != "reception_seule" || e.roleOf(t, b) != "reception_seule" || e.repair.RepairCalls != 2 {
		t.Fatalf("réparation : %s %s %d", e.roleOf(t, a), e.roleOf(t, b), e.repair.RepairCalls)
	}
}

func TestBatchAfterDrawKeepForFuture(t *testing.T) {
	e := newEnv(t)
	ck := e.adminCookies(t, e.groupA)
	a := e.person(t, e.groupA, "Alice", participants.RoleNormal)
	e.state.Done[e.groupA] = true
	e.repair.Previews = map[ports.RuleKind]ports.RepairPreview{ports.RuleRoleChange: {AffectedCount: 1, Repairable: true}}
	rec := e.post(t, "noel", url.Values{"ids": ids(a), "role": {"un_seul"}, "choice": {"future"}}, "fr", ck)
	if rec.Code != http.StatusSeeOther || !strings.Contains(rec.Header().Get("Location"), "future=1") {
		t.Fatalf("%d %q", rec.Code, rec.Header().Get("Location"))
	}
	if e.roleOf(t, a) != "un_seul" || e.repair.RepairCalls != 0 {
		t.Fatal("changement conservé sans réparation attendu")
	}
}

func TestApplyRefusals(t *testing.T) {
	e := newEnv(t)
	ck := e.adminCookies(t, e.groupA)
	a := e.person(t, e.groupA, "Alice", participants.RoleNormal)
	other := e.person(t, e.groupB, "Zoe", participants.RoleNormal)
	cases := []struct {
		name   string
		form   url.Values
		status int
		msg    string
	}{
		{"rôle inconnu", url.Values{"ids": ids(a), "role": {"roi"}}, 400, "Ce rôle n&#39;existe pas"},
		{"aucune sélection", url.Values{"role": {"normal"}}, 400, "Coche au moins une personne."},
		{"identifiant invalide", url.Values{"ids": {"x"}, "role": {"normal"}}, 400, "Cette personne n&#39;existe pas"},
		{"personne d'un autre groupe", url.Values{"ids": ids(other), "role": {"un_seul"}}, 404, "Cette personne n&#39;existe pas"},
	}
	for _, c := range cases {
		rec := e.post(t, "noel", c.form, "fr", ck)
		body := rec.Body.String()
		if rec.Code != c.status || !strings.Contains(body, c.msg) {
			t.Errorf("%s : %d", c.name, rec.Code)
		}
		if !strings.Contains(body, `role="alert"`) || !strings.Contains(body, `aria-invalid="true"`) {
			t.Errorf("%s : message sous le champ attendu", c.name)
		}
	}
	if e.roleOf(t, other) != "normal" || e.roleOf(t, a) != "normal" {
		t.Fatal("aucun rôle ne doit changer")
	}
	// une session d'un autre groupe ne change rien
	rec := e.postAs(t, "noel", url.Values{"ids": ids(a), "role": {"un_seul"}}, "fr", ck, e.adminCookies(t, e.groupB))
	if rec.Code != http.StatusSeeOther || e.roleOf(t, a) != "normal" {
		t.Fatalf("session d'un autre groupe : %d %s", rec.Code, e.roleOf(t, a))
	}
}

func TestSingleSelectorRedirectsOnlyToSameGroupAdmin(t *testing.T) {
	e := newEnv(t)
	ck := e.adminCookies(t, e.groupA)
	a := e.person(t, e.groupA, "Alice", participants.RoleNormal)
	for next, want := range map[string]string{
		"/noel/admin/participants/1": "/noel/admin/participants/1",
		"//evil.example":             "/noel/admin/roles?",
		"https://evil.example/":      "/noel/admin/roles?",
		"/paques/admin/participants": "/noel/admin/roles?",
	} {
		role := "un_seul"
		if e.roleOf(t, a) == "un_seul" {
			role = "normal"
		}
		rec := e.post(t, "noel", url.Values{"ids": ids(a), "role": {role}, "next": {next}}, "fr", ck)
		if loc := rec.Header().Get("Location"); !strings.HasPrefix(loc, want) {
			t.Errorf("next %q : %q", next, loc)
		}
	}
}

func TestFragmentsFilterAndBadge(t *testing.T) {
	e := newEnv(t)
	req := httpGet("/noel/admin/roles")
	clone, err := e.pages.tpl.Clone()
	if err != nil {
		t.Fatal(err)
	}
	clone.Funcs(testFuncs(e, req))
	var sb strings.Builder
	if err := clone.ExecuteTemplate(&sb, "admin/roles/filter", NewFilter("/noel/admin/participants", "offre_plus")); err != nil {
		t.Fatal(err)
	}
	out := sb.String()
	if !strings.Contains(out, `method="get"`) || !strings.Contains(out, `<option value="offre_plus" selected>`) || !strings.Contains(out, "Tous les rôles") {
		t.Errorf("filtre : %s", out)
	}
	sb.Reset()
	if err := clone.ExecuteTemplate(&sb, "admin/roles/badge", NewBadge(participants.RoleUnSeul)); err != nil || !strings.Contains(sb.String(), "Un seul cadeau") {
		t.Errorf("badge : %v %s", err, sb.String())
	}
	list := []participants.Participant{{ID: 1, Role: participants.RoleNormal}, {ID: 2, Role: participants.RoleUnSeul}}
	if got := FilterByRole(list, "un_seul"); len(got) != 1 || got[0].ID != 2 {
		t.Error("filtre par rôle")
	}
	if got := FilterByRole(list, ""); len(got) != 2 {
		t.Error("filtre vide = tous")
	}
	if got := FilterByRole(list, "roi"); len(got) != 2 {
		t.Error("rôle inconnu = tous")
	}
}
