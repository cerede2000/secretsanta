package groups

import (
	"context"
	"fmt"
	"testing"

	"secretsanta/internal/shared/errs"
)

// F2.AC9 : plafond de cadeaux par tireur, 2 par défaut, bornes 2 à 5,
// au-delà de 2 seulement avec confirmation explicite.

func TestMaxGiftsDefaultIsTwo(t *testing.T) {
	e := newEnv(t)
	r := mustCreate(t, e, validInput("famille"))
	if r.Group.MaxGiftsPerGiver != 2 {
		t.Fatal(r.Group.MaxGiftsPerGiver)
	}
	g, _ := e.svc.ByID(context.Background(), r.Group.ID)
	if g.MaxGiftsPerGiver != 2 || g.AdminCanViewPair {
		t.Fatalf("%+v", g)
	}
	// La confirmation seule ne change pas le défaut.
	in := validInput("famille-2")
	in.ConfirmMaxGifts = true
	if r := mustCreate(t, e, in); r.Group.MaxGiftsPerGiver != 2 {
		t.Fatal(r.Group.MaxGiftsPerGiver)
	}
}

func TestMaxGiftsOutOfBoundsRefused(t *testing.T) {
	e := newEnv(t)
	for _, v := range []int{-1, 0, 1, 6, 100} {
		for _, confirm := range []bool{false, true} {
			in := validInput("famille")
			in.MaxGiftsPerGiver = intp(v)
			in.ConfirmMaxGifts = confirm
			_, err := e.svc.Create(context.Background(), in)
			wantFrench(t, err, errs.KindValidation)
			if errs.UserMessage(err) != msgMaxGiftsRange {
				t.Fatalf("%d : %v", v, err)
			}
		}
	}
	if countGroups(t, e.db) != 0 {
		t.Fatal("plafond hors bornes enregistré")
	}
	if _, err := e.db.Exec(`INSERT INTO groups (slug, name, max_gifts_per_giver, admin_secret_hash, created_at) VALUES ('abc', 'x', 6, zeroblob(32), 'now')`); err == nil {
		t.Fatal("la base accepte un plafond de 6")
	}
}

func TestMaxGiftsAboveTwoNeedsConfirmation(t *testing.T) {
	e := newEnv(t)
	for _, v := range []int{3, 4, 5} {
		in := validInput(fmt.Sprintf("famille-%d", v))
		in.MaxGiftsPerGiver = intp(v)
		_, err := e.svc.Create(context.Background(), in)
		wantFrench(t, err, errs.KindValidation)
		if errs.UserMessage(err) != msgMaxGiftsConfirm {
			t.Fatalf("%d : %v", v, err)
		}
		in.ConfirmMaxGifts = true
		r := mustCreate(t, e, in)
		g, _ := e.svc.ByID(context.Background(), r.Group.ID)
		if g.MaxGiftsPerGiver != v {
			t.Fatalf("%d : %d", v, g.MaxGiftsPerGiver)
		}
	}
	// 2 explicite : pas de confirmation nécessaire.
	in := validInput("famille-deux")
	in.MaxGiftsPerGiver = intp(2)
	mustCreate(t, e, in)
}
