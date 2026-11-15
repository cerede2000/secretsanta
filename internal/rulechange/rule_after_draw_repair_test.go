package rulechange

import (
	"testing"

	"secretsanta/internal/shared/ports"
)

// F3d.AC5 : aperçu sans nom, puis écriture et réparation par le port.
func TestRuleAfterDrawRepair(t *testing.T) {
	e := newEnv(t, true)
	a, b := e.ids[0], e.ids[1]
	e.repair.Previews = map[ports.RuleKind]ports.RepairPreview{
		ports.RuleExclusion: {AffectedCount: 2, AlreadyDiscoveredCount: 1, Repairable: true},
	}

	o, err := e.svc.AddExclusion(t.Context(), e.gid, a, b, ChoiceAsk)
	if err != nil {
		t.Fatal(err)
	}
	if o.Status != StatusNeedsConfirmation || o.Preview.AffectedCount != 2 || o.Preview.AlreadyDiscoveredCount != 1 {
		t.Fatalf("aperçu : %+v", o)
	}
	noNames(t, o.Explanation)
	if n := e.count(t, `SELECT COUNT(*) FROM manual_exclusions`); n != 0 {
		t.Fatal("rien ne doit être écrit avant confirmation")
	}
	if e.repair.RepairCalls != 0 {
		t.Fatal("pas de réparation avant confirmation")
	}

	o, err = e.svc.AddExclusion(t.Context(), e.gid, a, b, ChoiceRepair)
	if err != nil || o.Status != StatusSaved || !o.Repaired {
		t.Fatalf("confirmation : %+v %v", o, err)
	}
	if e.repair.RepairCalls != 1 || len(e.repair.Committed) != 1 {
		t.Fatalf("réparation : %d %v", e.repair.RepairCalls, e.repair.Committed)
	}
	if got := e.repair.Committed[0]; got.Kind != ports.RuleExclusion || got.Members[0] != a || got.Members[1] != b {
		t.Fatalf("règle transmise : %+v", got)
	}
	if n := e.count(t, `SELECT COUNT(*) FROM manual_exclusions`); n != 1 {
		t.Fatal("la règle doit être écrite par le rappel d'écriture")
	}
	if f, _ := e.svc.Flags(t.Context(), e.gid); len(f) != 0 {
		t.Fatalf("pas d'indicateur après réparation : %v", f)
	}
}

func TestRuleAfterDrawRepairFailureKeepsError(t *testing.T) {
	e := newEnv(t, true)
	e.repair.Previews = map[ports.RuleKind]ports.RepairPreview{
		ports.RuleForcing: {AffectedCount: 1, Repairable: true},
	}
	e.repair.RepairErr = errBoom
	if _, err := e.svc.AddForced(t.Context(), e.gid, e.ids[0], e.ids[1], ChoiceRepair); err == nil {
		t.Fatal("l'échec de la réparation doit remonter")
	}
	if len(e.repair.Committed) != 0 {
		t.Fatal("rien ne doit être validé")
	}
}
