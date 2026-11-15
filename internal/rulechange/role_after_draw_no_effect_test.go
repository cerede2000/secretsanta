package rulechange

import (
	"testing"

	"secretsanta/internal/participants"
)

// F3d.AC8 : direct avant le tirage ; sans contradiction après, sans réparation.
func TestRoleBeforeDrawIsDirect(t *testing.T) {
	e := newEnv(t, false)
	e.repair.RepairErr = errBoom // ne doit jamais être appelée
	o, err := e.svc.SetRole(t.Context(), e.gid, e.ids[0], participants.RoleReceptionSeule, ChoiceAsk)
	if err != nil || o.Status != StatusSaved {
		t.Fatalf("%+v %v", o, err)
	}
	if e.role(t, e.ids[0]) != "reception_seule" || e.repair.RepairCalls != 0 {
		t.Fatal("écriture directe attendue")
	}
}

func TestRoleAfterDrawNoEffect(t *testing.T) {
	e := newEnv(t, true)
	o, err := e.svc.SetRole(t.Context(), e.gid, e.ids[0], participants.RoleOffrePlus, ChoiceAsk)
	if err != nil || o.Status != StatusSaved || o.Repaired {
		t.Fatalf("%+v %v", o, err)
	}
	if e.role(t, e.ids[0]) != "offre_plus" || e.repair.RepairCalls != 0 {
		t.Fatal("rôle écrit sans réparation attendu")
	}
	if f, _ := e.svc.Flags(t.Context(), e.gid); len(f) != 0 {
		t.Fatalf("indicateurs : %v", f)
	}
	if _, err := e.svc.SetRole(t.Context(), e.gid, e.ids[0], "inconnu", ChoiceAsk); err == nil {
		t.Fatal("rôle inconnu refusé")
	}
}
