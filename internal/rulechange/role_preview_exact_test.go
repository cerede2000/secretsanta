package rulechange

import (
	"context"
	"testing"

	"secretsanta/internal/participants"
	"secretsanta/internal/shared/ports"
)

// rolePreviewer est une réparation qui offre l'aperçu exact d'un rôle : il
// dépend du rôle demandé, ce que l'aperçu prudent de Preview ne peut pas faire.
type rolePreviewer struct {
	*ports.FakeRuleChangeRepair
	asked []participants.DrawRole
}

func (r *rolePreviewer) PreviewRole(_ context.Context, _, _ int64, role participants.DrawRole) (ports.RepairPreview, error) {
	r.asked = append(r.asked, role)
	if role == participants.RoleReceptionSeule {
		return ports.RepairPreview{AffectedCount: 3, AlreadyDiscoveredCount: 1, Repairable: true}, nil
	}
	return ports.RepairPreview{Repairable: true}, nil
}

// SetRole utilise l'aperçu exact de la réparation (RolePreviewer) : le rôle
// demandé change le résultat, et un rôle sans effet est enregistré directement.
func TestSetRoleUsesExactRolePreview(t *testing.T) {
	e := newEnv(t, true)
	rp := &rolePreviewer{FakeRuleChangeRepair: e.repair}
	e.svc = NewService(e.db, e.svc.clock, e.draw, rp)
	// L'aperçu prudent dirait « 9 personnes » : il ne doit pas servir.
	e.repair.Previews = map[ports.RuleKind]ports.RepairPreview{ports.RuleRoleChange: {AffectedCount: 9, Repairable: true}}

	o, err := e.svc.SetRole(t.Context(), e.gid, e.ids[1], participants.RoleReceptionSeule, ChoiceAsk)
	if err != nil || o.Status != StatusNeedsConfirmation || o.Preview.AffectedCount != 3 || o.Preview.AlreadyDiscoveredCount != 1 {
		t.Fatalf("aperçu exact attendu (3, 1) : %+v %v", o, err)
	}
	if e.role(t, e.ids[1]) != "normal" {
		t.Fatal("rien ne doit être écrit avant confirmation")
	}
	o, err = e.svc.SetRole(t.Context(), e.gid, e.ids[2], participants.RoleUnSeul, ChoiceAsk)
	if err != nil || o.Status != StatusSaved || e.role(t, e.ids[2]) != string(participants.RoleUnSeul) {
		t.Fatalf("un rôle sans effet doit être enregistré : %+v %v", o, err)
	}
	if len(rp.asked) != 2 || rp.asked[0] != participants.RoleReceptionSeule || rp.asked[1] != participants.RoleUnSeul {
		t.Fatalf("rôles demandés à PreviewRole : %v", rp.asked)
	}
}
