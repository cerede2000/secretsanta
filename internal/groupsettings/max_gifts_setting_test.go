package groupsettings

import (
	"context"
	"testing"

	"secretsanta/internal/shared/errs"
)

func TestMaxGiftsSetting(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	e.draw.Done[e.gid] = true
	set := func(m int, confirm bool) (*Result, error) {
		in := e.in()
		in.MaxGiftsPerGiver, in.ConfirmMaxGifts = &m, confirm
		return e.svc.UpdateGroupSettings(ctx, in)
	}
	if _, err := set(3, false); !errs.Is(err, errs.KindValidation) {
		t.Fatalf("confirmation exigée au-delà de 2 : %v", err)
	}
	for _, m := range []int{1, 6, 0, -1} {
		if _, err := set(m, true); !errs.Is(err, errs.KindValidation) {
			t.Fatalf("%d hors bornes : %v", m, err)
		}
	}
	for _, m := range []int{5, 3, 2} {
		r, err := set(m, m > 2)
		if err != nil || r.Group.MaxGiftsPerGiver != m {
			t.Fatalf("plafond %d : %+v %v", m, r, err)
		}
	}
	// nil laisse le plafond inchangé.
	if _, err := set(4, true); err != nil {
		t.Fatal(err)
	}
	r, err := e.svc.UpdateGroupSettings(ctx, e.in())
	if err != nil || r.Group.MaxGiftsPerGiver != 4 {
		t.Fatalf("plafond inchangé attendu : %+v %v", r, err)
	}
	if len(e.bus.Emitted) != 0 || r.Group.BudgetVersion != 0 {
		t.Fatalf("aucun événement de budget attendu : %d, version %d", len(e.bus.Emitted), r.Group.BudgetVersion)
	}
}
