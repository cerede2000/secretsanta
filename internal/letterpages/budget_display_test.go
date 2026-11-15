package letterpages

import (
	"strings"
	"testing"

	"secretsanta/internal/participants"
)

func TestReceivedLetterBudgetOptionalAndLive(t *testing.T) {
	e := newEnv(t)
	a := e.add(t, "Alice", participants.RoleNormal)
	e.add(t, "Bobby", participants.RoleNormal)
	e.add(t, "Carla", participants.RoleNormal)
	if _, err := e.letters.SetIntro(ctx, e.gid, a, "Mon message"); err != nil {
		t.Fatal(err)
	}
	ps := e.revealed(t)
	var pair rawPair
	for _, p := range ps {
		if p.Receiver == a {
			pair = p
		}
	}
	c := e.login(t, pair.Giver)
	url := "/noel/lettre-recue/" + itoa(pair.ID)
	version := e.letterVersion(t, a)

	hasBudget := func(body string) bool {
		return strings.Contains(body, "Budget du groupe") || strings.Contains(body, "letter-budget")
	}
	// Sans budget : ni mention, ni espace réservé.
	body := e.get(t, url, c).Body.String()
	if hasBudget(body) || strings.Contains(body, "€") {
		t.Fatal("budget affiché alors qu'il est vide")
	}
	// Défini : affiché.
	if _, err := e.db.Exec(`UPDATE groups SET budget_cents = 2500, budget_version = budget_version + 1 WHERE id = ?`, e.gid); err != nil {
		t.Fatal(err)
	}
	body = e.get(t, url, c).Body.String()
	if !hasBudget(body) || !strings.Contains(body, "25") {
		t.Fatal("budget défini non affiché")
	}
	// Changé : suivi aussitôt.
	if _, err := e.db.Exec(`UPDATE groups SET budget_cents = 3050, budget_version = budget_version + 1 WHERE id = ?`, e.gid); err != nil {
		t.Fatal(err)
	}
	if body = e.get(t, url, c).Body.String(); !strings.Contains(body, "30,50") {
		t.Fatal("changement de budget non suivi")
	}
	// Retiré : plus aucune mention.
	if _, err := e.db.Exec(`UPDATE groups SET budget_cents = NULL, budget_version = budget_version + 1 WHERE id = ?`, e.gid); err != nil {
		t.Fatal(err)
	}
	if body = e.get(t, url, c).Body.String(); hasBudget(body) || strings.Contains(body, "€") {
		t.Fatal("budget retiré encore affiché")
	}
	// La lettre et sa version n'ont pas bougé.
	if v := e.letterVersion(t, a); v != version {
		t.Fatalf("version de la lettre %d -> %d", version, v)
	}
	if !strings.Contains(body, "Mon message") {
		t.Fatal("lettre absente")
	}
}
