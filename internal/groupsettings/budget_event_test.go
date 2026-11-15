package groupsettings

import (
	"context"
	"strings"
	"testing"
)

func TestBudgetEvent(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	in := e.in()
	in.Budget, in.NotifyBudget = "20", true
	if _, err := e.svc.UpdateGroupSettings(ctx, in); err != nil {
		t.Fatal(err)
	}
	if len(e.bus.Emitted) != 1 {
		t.Fatalf("un événement attendu, obtenu %d", len(e.bus.Emitted))
	}
	ev := e.bus.Emitted[0]
	if ev.Name != "budget_modifie" || ev.GroupID != e.gid || ev.Payload["old_budget"] != "aucun" ||
		ev.Payload["new_budget"] != "2000" || ev.Payload["notify"] != "true" {
		t.Fatalf("événement inattendu : %+v", ev)
	}

	// Sans « prévenir » : l'événement porte notify=false.
	in.Budget, in.NotifyBudget = "", false
	if _, err := e.svc.UpdateGroupSettings(ctx, in); err != nil {
		t.Fatal(err)
	}
	ev = e.bus.Emitted[1]
	if ev.Payload["old_budget"] != "2000" || ev.Payload["new_budget"] != "aucun" || ev.Payload["notify"] != "false" {
		t.Fatalf("événement inattendu : %+v", ev)
	}

	// Pas de changement de budget : pas d'événement.
	in.Name = "Autre"
	if _, err := e.svc.UpdateGroupSettings(ctx, in); err != nil {
		t.Fatal(err)
	}
	if len(e.bus.Emitted) != 2 {
		t.Fatalf("aucun événement attendu sans changement, obtenu %d", len(e.bus.Emitted))
	}

	// Aucune donnée personnelle : seules les clés connues, valeurs numériques ou booléennes.
	for _, ev := range e.bus.Emitted {
		for k, v := range ev.Payload {
			switch k {
			case "old_budget", "new_budget", "notify", "version":
			default:
				t.Fatalf("clé inattendue %q", k)
			}
			if strings.ContainsAny(v, "@ ") && v != "aucun" {
				t.Fatalf("valeur suspecte %q", v)
			}
		}
	}
}
