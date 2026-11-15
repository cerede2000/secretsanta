package rulechange

import (
	"testing"

	"secretsanta/internal/participants"
	"secretsanta/internal/shared/ports"
)

// F3d.AC9 : aperçu en comptes seulement, puis rôle et réparation en une transaction.
func TestRoleAfterDrawRepair(t *testing.T) {
	e := newEnv(t, true)
	id := e.ids[1]
	e.repair.Previews = map[ports.RuleKind]ports.RepairPreview{
		ports.RuleRoleChange: {AffectedCount: 2, AlreadyDiscoveredCount: 2, Repairable: true},
	}
	o, err := e.svc.SetRole(t.Context(), e.gid, id, participants.RoleReceptionSeule, ChoiceAsk)
	if err != nil || o.Status != StatusNeedsConfirmation || o.Preview.AffectedCount != 2 {
		t.Fatalf("%+v %v", o, err)
	}
	noNames(t, o.Explanation)
	if e.role(t, id) != "normal" {
		t.Fatal("rien ne doit être écrit avant confirmation")
	}
	o, err = e.svc.SetRole(t.Context(), e.gid, id, participants.RoleReceptionSeule, ChoiceRepair)
	if err != nil || !o.Repaired || o.Status != StatusSaved {
		t.Fatalf("%+v %v", o, err)
	}
	if e.role(t, id) != "reception_seule" {
		t.Fatal("rôle non écrit")
	}
	if e.repair.RepairCalls != 1 || len(e.repair.Committed) != 1 ||
		e.repair.Committed[0].Kind != ports.RuleRoleChange || e.repair.Committed[0].Members[0] != id {
		t.Fatalf("réparation : %+v", e.repair.Committed)
	}
}
