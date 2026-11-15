package groupsettings

import (
	"context"
	"testing"

	"secretsanta/internal/shared/errs"
)

func TestSettingsAfterDraw(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	e.draw.Done[e.gid] = true
	if _, err := e.db.Exec(`CREATE TABLE pairs_probe (giver INTEGER, receiver INTEGER)`); err != nil {
		t.Fatal(err)
	}
	if _, err := e.db.Exec(`INSERT INTO pairs_probe VALUES (1, 2)`); err != nil {
		t.Fatal(err)
	}

	in := e.in()
	in.Budget, in.Name, in.EventDate, in.GiftInstructions = "25,50", "Nouveau nom", "2026-12-24", "Pas de chocolat."
	r, err := e.svc.UpdateGroupSettings(ctx, in)
	if err != nil {
		t.Fatal(err)
	}
	g := r.Group
	if g.BudgetCents == nil || *g.BudgetCents != 2550 || g.BudgetVersion != 1 || g.Name != "Nouveau nom" ||
		g.EventDate != "2026-12-24" || g.GiftInstructions != "Pas de chocolat." || g.Slug != e.slug {
		t.Fatalf("réglages inattendus : %+v", g)
	}

	// Modification du budget : version 2.
	in.Budget = "30"
	if r, err = e.svc.UpdateGroupSettings(ctx, in); err != nil || r.Group.BudgetVersion != 2 {
		t.Fatalf("version 2 attendue : %+v %v", r, err)
	}
	// Autre réglage sans changement de budget : version inchangée.
	in.Name = "Encore un nom"
	if r, err = e.svc.UpdateGroupSettings(ctx, in); err != nil || r.Group.BudgetVersion != 2 || r.BudgetChanged {
		t.Fatalf("version inchangée attendue : %+v %v", r, err)
	}
	// Retrait du budget et de la date : version 3.
	in.Budget, in.EventDate = "", ""
	if r, err = e.svc.UpdateGroupSettings(ctx, in); err != nil || r.Group.BudgetCents != nil || r.Group.BudgetVersion != 3 || r.Group.EventDate != "" {
		t.Fatalf("retrait attendu : %+v %v", r, err)
	}

	var gi, ri int
	if err := e.db.QueryRow(`SELECT giver, receiver FROM pairs_probe`).Scan(&gi, &ri); err != nil || gi != 1 || ri != 2 {
		t.Fatalf("paires modifiées : %d %d %v", gi, ri, err)
	}
	var n int
	_ = e.db.QueryRow(`SELECT COUNT(*) FROM pairs_probe`).Scan(&n)
	if n != 1 {
		t.Fatalf("paires : %d", n)
	}
}

func TestSettingsValidation(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	for name, mod := range map[string]func(*Input){
		"nom vide":       func(i *Input) { i.Name = "  " },
		"budget négatif": func(i *Input) { i.Budget = "-5" },
		"budget zéro":    func(i *Input) { i.Budget = "0" },
		"budget énorme":  func(i *Input) { i.Budget = "999999" },
		"date invalide":  func(i *Input) { i.EventDate = "24/12/2026" },
	} {
		in := e.in()
		mod(&in)
		_, err := e.svc.UpdateGroupSettings(ctx, in)
		if !errs.Is(err, errs.KindValidation) {
			t.Fatalf("%s : validation attendue, obtenu %v", name, err)
		}
	}
	in := e.in()
	in.GroupID = 9999
	if _, err := e.svc.UpdateGroupSettings(ctx, in); !errs.Is(err, errs.KindNotFound) {
		t.Fatalf("groupe inconnu : %v", err)
	}
}
