package rulechange

import (
	"errors"
	"strings"
	"testing"

	"secretsanta/internal/shared/ports"
)

var errBoom = errors.New("échec")

// F3d.AC6 : sans réparation possible ou refusée, la règle est conservée avec l'indicateur.
func TestRuleAfterDrawFutureOnly(t *testing.T) {
	for _, tc := range []struct {
		name       string
		repairable bool
		choice     Choice
	}{
		{"aucune réparation", false, ChoiceAsk},
		{"réparation refusée", true, ChoiceFutureOnly},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := newEnv(t, true)
			a, b := e.ids[0], e.ids[1]
			e.repair.Previews = map[ports.RuleKind]ports.RepairPreview{
				ports.RuleExclusion: {AffectedCount: 1, Repairable: tc.repairable},
			}
			o, err := e.svc.AddExclusion(t.Context(), e.gid, a, b, tc.choice)
			if err != nil || o.Status != StatusSavedFutureOnly {
				t.Fatalf("%+v %v", o, err)
			}
			if !strings.Contains(o.Explanation, "n'affecte pas le tirage actuel") ||
				!strings.Contains(o.Explanation, "nouveaux tirages") {
				t.Fatalf("explication : %q", o.Explanation)
			}
			noNames(t, o.Explanation)
			if e.repair.RepairCalls != 0 {
				t.Fatal("pas de réparation")
			}
			if n := e.count(t, `SELECT COUNT(*) FROM manual_exclusions`); n != 1 {
				t.Fatal("la règle doit être conservée")
			}
			f, err := e.svc.Flags(t.Context(), e.gid)
			if err != nil || len(f) != 1 || f[0] != (Flag{ports.RuleExclusion, a, b}) {
				t.Fatalf("indicateurs : %v %v", f, err)
			}
			// Retirer la règle retire l'indicateur ; un nouveau tirage les efface.
			if err := e.svc.RemoveExclusion(t.Context(), e.gid, a, b); err != nil {
				t.Fatal(err)
			}
			if f, _ := e.svc.Flags(t.Context(), e.gid); len(f) != 0 {
				t.Fatalf("indicateur restant : %v", f)
			}
		})
	}
}

func TestHouseholdFutureOnlyAndClear(t *testing.T) {
	e := newEnv(t, true)
	e.repair.Previews = map[ports.RuleKind]ports.RepairPreview{
		ports.RuleHousehold: {AffectedCount: 3, Repairable: false},
	}
	o, err := e.svc.CreateHousehold(t.Context(), e.gid, "F", e.ids[:2], ChoiceAsk)
	if err != nil || o.Status != StatusSavedFutureOnly || o.HouseholdID == 0 {
		t.Fatalf("%+v %v", o, err)
	}
	f, _ := e.svc.Flags(t.Context(), e.gid)
	if len(f) != 1 || f[0].Kind != ports.RuleHousehold || f[0].A != o.HouseholdID {
		t.Fatalf("indicateurs : %v", f)
	}
	if err := e.svc.ClearFlags(t.Context(), e.gid); err != nil {
		t.Fatal(err)
	}
	if f, _ := e.svc.Flags(t.Context(), e.gid); len(f) != 0 {
		t.Fatal("indicateurs non effacés")
	}
}
