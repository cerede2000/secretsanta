package groups

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"secretsanta/internal/shared/errs"
)

// F2.AC6 : budget facultatif ; vide = aucun budget affiché nulle part ;
// négatif, non numérique ou hors borne refusé avec un message français.

func TestGroupCreatedWithoutBudget(t *testing.T) {
	e := newEnv(t)
	for i, empty := range []string{"", "   ", "\x00", " € "} {
		in := validInput(fmt.Sprintf("sans-montant-%d", i))
		in.Budget = empty
		r := mustCreate(t, e, in)
		if r.Group.BudgetCents != nil || r.Group.HasBudget() || r.Group.BudgetLabel() != "" {
			t.Fatalf("%q : budget inattendu %+v", empty, r.Group)
		}
		var isNull bool
		if err := e.db.QueryRow(`SELECT budget_cents IS NULL FROM groups WHERE id = ?`, r.Group.ID).Scan(&isNull); err != nil || !isNull {
			t.Fatalf("budget non NULL en base (%v)", err)
		}
		g, err := e.svc.ByID(context.Background(), r.Group.ID)
		if err != nil || g.HasBudget() || g.BudgetLabel() != "" || g.BudgetVersion != 0 {
			t.Fatalf("relu : %+v %v", g, err)
		}
		pv, _ := e.svc.PublicPage(context.Background(), in.Slug)
		b, _ := json.Marshal(pv)
		if strings.Contains(strings.ToLower(string(b)), "budget") || strings.Contains(string(b), "€") {
			t.Fatalf("la page publique mentionne un budget : %s", b)
		}
	}
	// Par défaut (champ non renseigné), aucun budget.
	r := mustCreate(t, e, validInput("defaut"))
	if r.Group.HasBudget() {
		t.Fatal("budget par défaut non vide")
	}
}

func TestBudgetAccepted(t *testing.T) {
	cases := map[string]int64{
		"20": 2000, "25,50": 2550, "25.5": 2550, " 30 € ": 3000, "1": 100, "10000": 1_000_000, "10 000": 1_000_000,
	}
	for in, want := range cases {
		got, err := ParseBudget(in)
		if err != nil || got == nil || *got != want {
			t.Fatalf("%q : %v %v", in, got, err)
		}
	}
	e := newEnv(t)
	in := validInput("avec-budget")
	in.Budget = "25,50"
	r := mustCreate(t, e, in)
	if r.Group.BudgetLabel() != "25,50 €" {
		t.Fatalf("%q", r.Group.BudgetLabel())
	}
	if FormatEuros(1_000_000) != "10 000 €" {
		t.Fatalf("%q", FormatEuros(1_000_000))
	}
}

func TestBudgetRefused(t *testing.T) {
	e := newEnv(t)
	cases := map[string]string{
		"-5":        msgBudgetNegative,
		"-0,01":     msgBudgetNegative,
		"abc":       msgBudgetFormat,
		"20 euros":  msgBudgetFormat,
		"1e3":       msgBudgetFormat,
		"12,345":    msgBudgetFormat,
		"0x10":      msgBudgetFormat,
		"1,2,3":     msgBudgetFormat,
		"NaN":       msgBudgetFormat,
		"0":         msgBudgetRange,
		"0,50":      msgBudgetRange,
		"10000,01":  msgBudgetRange,
		"999999999": msgBudgetFormat,
		"20000":     msgBudgetRange,
	}
	for in, want := range cases {
		cin := validInput("famille")
		cin.Budget = in
		_, err := e.svc.Create(context.Background(), cin)
		wantFrench(t, err, errs.KindValidation)
		if errs.UserMessage(err) != want {
			t.Fatalf("%q : %q au lieu de %q", in, errs.UserMessage(err), want)
		}
	}
	if countGroups(t, e.db) != 0 {
		t.Fatal("budget invalide enregistré")
	}
}
