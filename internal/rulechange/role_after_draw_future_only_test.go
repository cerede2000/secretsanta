package rulechange

import (
	"strings"
	"testing"

	"secretsanta/internal/participants"
	"secretsanta/internal/shared/ports"
)

// F3d.AC10 : sans réparation ou réparation refusée, le rôle est conservé avec l'indicateur.
func TestRoleAfterDrawFutureOnly(t *testing.T) {
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
			id := e.ids[2]
			e.repair.Previews = map[ports.RuleKind]ports.RepairPreview{
				ports.RuleRoleChange: {AffectedCount: 1, Repairable: tc.repairable},
			}
			o, err := e.svc.SetRole(t.Context(), e.gid, id, participants.RoleUnSeul, tc.choice)
			if err != nil || o.Status != StatusSavedFutureOnly {
				t.Fatalf("%+v %v", o, err)
			}
			if !strings.Contains(o.Explanation, "n'affecte pas le tirage actuel") ||
				!strings.Contains(o.Explanation, "futurs ajustements") {
				t.Fatalf("explication : %q", o.Explanation)
			}
			if e.role(t, id) != "un_seul" || e.repair.RepairCalls != 0 {
				t.Fatal("rôle conservé sans réparation attendu")
			}
			f, _ := e.svc.Flags(t.Context(), e.gid)
			if len(f) != 1 || f[0] != (Flag{ports.RuleRoleChange, id, 0}) {
				t.Fatalf("indicateurs : %v", f)
			}
			// Un nouveau changement sans contradiction efface l'indicateur.
			e.repair.Previews = nil
			if _, err := e.svc.SetRole(t.Context(), e.gid, id, participants.RoleNormal, ChoiceAsk); err != nil {
				t.Fatal(err)
			}
			if f, _ := e.svc.Flags(t.Context(), e.gid); len(f) != 0 {
				t.Fatalf("indicateur non effacé : %v", f)
			}
		})
	}
}
