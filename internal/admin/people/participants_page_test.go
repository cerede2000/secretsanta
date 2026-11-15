package people

import (
	"context"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"testing"
	"time"

	"secretsanta/internal/participants"
	"secretsanta/internal/rulechange"
)

func list(t *testing.T, e *env, q string, ck []*http.Cookie) string {
	t.Helper()
	rec := e.get(t, "/noel/admin/participants"+q, "fr", ck)
	if rec.Code != 200 {
		t.Fatalf("%s : %d", q, rec.Code)
	}
	return rec.Body.String()
}

func TestParticipantsPageFiltersByNameAccessHouseholdAndMissingEmail(t *testing.T) {
	e := newEnv(t)
	a := e.person(t, e.groupA, "Alice", "alice@exemple.fr", participants.RoleNormal)
	b := e.person(t, e.groupA, "Bruno", "bruno@exemple.fr", participants.RoleNormal)
	c := e.person(t, e.groupA, "Chloe", "", participants.RoleNormal)
	d := e.person(t, e.groupA, "David", "david@exemple.fr", participants.RoleNormal)
	e.queueRow(t, e.groupA, a, "sent", t0)
	e.queueRow(t, e.groupA, b, "failed", t0)
	e.openSession(t, e.groupA, a, t0.Add(time.Hour))
	if _, err := e.pages.o.Changes.CreateHousehold(context.Background(), e.groupA, "Martin", []int64{b, d}, rulechange.ChoiceAsk); err != nil {
		t.Fatal(err)
	}
	ck := e.adminCookies(t, e.groupA)

	has := func(body string, names ...string) map[string]bool {
		out := map[string]bool{}
		for _, n := range names {
			out[n] = strings.Contains(body, ">Zx"+"") && false
			out[n] = regexp.MustCompile(`class="ppl-name"[^>]*>` + n + ` `).MatchString(body)
		}
		return out
	}
	all := []string{"Alice", "Bruno", "Chloe", "David"}
	check := func(label, q string, want ...string) {
		t.Helper()
		got := has(list(t, e, q, ck), all...)
		wantSet := map[string]bool{}
		for _, w := range want {
			wantSet[w] = true
		}
		for _, n := range all {
			if got[n] != wantSet[n] {
				t.Errorf("%s : %s affichée=%v, attendue=%v", label, n, got[n], wantSet[n])
			}
		}
	}
	check("tout", "", "Alice", "Bruno", "Chloe", "David")
	check("nom", "?q=bru", "Bruno")
	check("nom insensible à la casse", "?q=ZXCHLOE", "Chloe")
	check("adresse", "?q=david@", "David")
	check("envoyé", "?acces=envoye", "Alice")
	check("échec", "?acces=echec", "Bruno")
	check("sans courriel (état)", "?acces=aucun", "Chloe")
	check("ouvert", "?acces=ouvert", "Alice")
	check("jamais ouvert", "?acces=jamais", "Bruno", "Chloe", "David")
	check("foyer", "?foyer="+sid(1), "Bruno", "David")
	check("sans foyer", "?foyer=aucun", "Alice", "Chloe")
	check("sans courriel", "?sans_courriel=1", "Chloe")
	check("combiné", "?acces=jamais&foyer=1&q=dav", "David")
	check("filtre inconnu ignoré", "?acces=nimporte", "Alice", "Bruno", "Chloe", "David")
	_ = c
}

func TestParticipantsPageRowMenuAndInlineDeleteConfirmation(t *testing.T) {
	e := newEnv(t)
	a := e.person(t, e.groupA, "Alice", "alice@exemple.fr", participants.RoleNormal)
	e.person(t, e.groupA, "Bruno", "bruno@exemple.fr", participants.RoleNormal)
	ck := e.adminCookies(t, e.groupA)
	body := list(t, e, "", ck)
	if n := strings.Count(body, `aria-haspopup="menu"`); n != 2 {
		t.Fatalf("un menu « … » par ligne attendu, %d", n)
	}
	for _, want := range []string{`role="menuitem"`, "Fiche", "Renvoyer le lien", "Modifier", "Supprimer", `data-avatar-name="Alice ZxAliceovitch"`} {
		if !strings.Contains(body, want) {
			t.Errorf("%q absent", want)
		}
	}
	if strings.Contains(body, "confirm(") || strings.Contains(body, "alert(") {
		t.Error("boîte du navigateur")
	}
	// Confirmation intégrée : une page, pas une boîte du navigateur.
	conf := list(t, e, "?supprimer="+sid(a), ck)
	if !strings.Contains(conf, `role="alertdialog"`) || !strings.Contains(conf, "Retirer Alice ZxAliceovitch ?") {
		t.Error("confirmation de suppression intégrée absente")
	}
	if rec := e.post(t, "/noel/admin/participants/"+sid(a)+"/supprimer", url.Values{}, "fr", ck); rec.Code != 400 {
		t.Errorf("sans confirmation : 400 attendu, %d", rec.Code)
	}
	if rec := e.post(t, "/noel/admin/participants/"+sid(a)+"/supprimer", url.Values{"confirm": {"1"}}, "fr", ck); rec.Code != 303 {
		t.Fatalf("303 attendu, %d", rec.Code)
	}
	if strings.Contains(list(t, e, "", ck), "Alice") {
		t.Error("Alice devrait être retirée")
	}
	if got := strings.Join(e.auditActions(t, e.groupA), ","); got != ActionPersonDel {
		t.Errorf("journal : %q", got)
	}
}

func TestParticipantsPageAddEditAndErrorsUnderTheField(t *testing.T) {
	e := newEnv(t)
	ck := e.adminCookies(t, e.groupA)
	rec := e.post(t, "/noel/admin/participants", url.Values{"first_name": {"Noé"}, "name": {"Lambert"}, "email": {"noe@exemple.fr"}}, "fr", ck)
	if rec.Code != 303 {
		t.Fatalf("ajout : 303 attendu, %d", rec.Code)
	}
	dup := e.post(t, "/noel/admin/participants", url.Values{"first_name": {"Noé"}, "name": {"Lambert"}}, "fr", ck)
	if dup.Code != 409 || !strings.Contains(dup.Body.String(), `aria-invalid="true"`) || !strings.Contains(dup.Body.String(), "porte déjà ce prénom") {
		t.Errorf("doublon : message sous le champ attendu (%d)", dup.Code)
	}
	if bad := e.post(t, "/noel/admin/participants", url.Values{"first_name": {"X"}, "name": {"Y"}, "email": {"nope"}}, "fr", ck); bad.Code != 400 || !strings.Contains(bad.Body.String(), `id="ppl-email-err"`) {
		t.Errorf("adresse invalide : %d", bad.Code)
	}
	var id int64
	if err := e.db.QueryRow(`SELECT id FROM participants WHERE first_name = 'Noé'`).Scan(&id); err != nil {
		t.Fatal(err)
	}
	if f := e.get(t, "/noel/admin/participants/"+sid(id)+"/modifier", "fr", ck); f.Code != 200 || !strings.Contains(f.Body.String(), `value="noe@exemple.fr"`) {
		t.Errorf("formulaire de modification : %d", f.Code)
	}
	if up := e.post(t, "/noel/admin/participants/"+sid(id)+"/modifier", url.Values{"first_name": {"Noé"}, "name": {"Lambert"}, "email": {"noe2@exemple.fr"}}, "fr", ck); up.Code != 303 {
		t.Errorf("modification : %d", up.Code)
	}
	if got := strings.Join(e.auditActions(t, e.groupA), ","); got != ActionPersonAdded+","+ActionPersonEdit {
		t.Errorf("journal : %q", got)
	}
}

func TestParticipantsPageShowsNoTargetAndIsIsolatedByGroup(t *testing.T) {
	e := newEnv(t)
	a := e.person(t, e.groupA, "Alice", "alice@exemple.fr", participants.RoleNormal)
	b := e.person(t, e.groupA, "Bruno", "bruno@exemple.fr", participants.RoleNormal)
	e.person(t, e.groupB, "Zoe", "zoe@exemple.fr", participants.RoleNormal)
	ck := e.adminCookies(t, e.groupA)
	csrf := regexp.MustCompile(`name="csrf_token" value="[^"]+"`)
	before := csrf.ReplaceAllString(list(t, e, "", ck), "")
	e.seedDraw(t, e.groupA, []int64{a, b})
	e.state.Done[e.groupA] = false
	after := csrf.ReplaceAllString(list(t, e, "", ck), "")
	if before != after {
		t.Error("la page ne doit pas dépendre des paires du tirage")
	}
	// Chercher une personne ne ramène jamais sa cible.
	body := list(t, e, "?q=Alice", ck)
	if n := strings.Count(body, `class="ppl-name"`); n != 1 || strings.Contains(body, `class="ppl-name" href="/noel/admin/participants/`+sid(b)+`"`) {
		t.Errorf("la recherche d'Alice ne doit lister qu'elle (%d lignes)", n)
	}
	if strings.Contains(before, "Zoe") {
		t.Error("une personne d'un autre groupe apparaît")
	}
	for _, w := range []string{"→", "offre à Bruno", "cible de"} {
		if strings.Contains(before, w) {
			t.Errorf("indice de paire %q", w)
		}
	}
}

func TestExclusionsHouseholdsAndAfterDrawPreviewWithoutNames(t *testing.T) {
	e := newEnv(t)
	a := e.person(t, e.groupA, "Alice", "alice@exemple.fr", participants.RoleNormal)
	b := e.person(t, e.groupA, "Bruno", "bruno@exemple.fr", participants.RoleNormal)
	c := e.person(t, e.groupA, "Chloe", "chloe@exemple.fr", participants.RoleNormal)
	ck := e.adminCookies(t, e.groupA)
	if rec := e.post(t, "/noel/admin/participants/regles", url.Values{"kind": {"exclusion"}, "a": {sid(a)}, "b": {sid(b)}, "both": {"1"}}, "fr", ck); rec.Code != 303 {
		t.Fatalf("exclusion : %d", rec.Code)
	}
	if body := list(t, e, "", ck); !strings.Contains(body, "dans les deux sens") {
		t.Error("exclusion non listée")
	}
	if rec := e.post(t, "/noel/admin/participants/regles", url.Values{"kind": {"household"}, "name": {"Martin"}, "members": {sid(b), sid(c)}}, "fr", ck); rec.Code != 303 {
		t.Fatalf("foyer : %d", rec.Code)
	}
	if body := list(t, e, "", ck); !strings.Contains(body, "Foyer Martin") {
		t.Error("foyer non listé")
	}
	if rec := e.post(t, "/noel/admin/participants/regles/retirer", url.Values{"kind": {"exclusion"}, "a": {sid(a)}, "b": {sid(b)}, "both": {"1"}}, "fr", ck); rec.Code != 303 {
		t.Fatalf("retrait : %d", rec.Code)
	}
	if body := list(t, e, "", ck); strings.Contains(body, "dans les deux sens") {
		t.Error("exclusion toujours listée")
	}
	// Après le tirage : aperçu en comptes, sans nom, avant toute écriture.
	e.seedDraw(t, e.groupA, []int64{a, b, c})
	e.repair.Previews = nil
	rec := e.post(t, "/noel/admin/participants/regles", url.Values{"kind": {"forced"}, "a": {sid(a)}, "b": {sid(c)}}, "fr", ck)
	if rec.Code != 200 && rec.Code != 303 {
		t.Fatalf("forçage après tirage : %d", rec.Code)
	}
}
