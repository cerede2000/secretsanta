package drawengine

import (
	"testing"

	"pgregory.net/rapid"
)

// F4.AC8 : pour toute instance générée avec des rôles variés, chaque
// participant reçoit exactement un cadeau, chaque tireur en offre au moins
// un, aucune reception_seule n'en offre et aucun tireur ne dépasse le
// plafond.
func TestPropertyCapacities(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		in := genInput(t, genOpts{minN: 2, maxN: 9, roles: true, capChoices: []int{0, 1, 2, 3, 4}})
		o := newOracle(in)
		res, err := Draw(in)
		if !o.ok {
			if err == nil {
				t.Fatalf("instance irrecevable acceptée : %+v", in)
			}
			asInfeasible(t, err)
			assertNoIdentity(t, in, err)
			return
		}
		if in.Participants != nil && len(o.enumerate(1)) == 0 {
			// Seul l'anti-échange peut encore rendre l'instance impossible.
			if err == nil {
				t.Fatal("tirage accepté sans solution")
			}
			return
		}
		if err != nil {
			t.Fatalf("tirage refusé : %v", err)
		}
		received := map[int64]int{}
		given := map[int64]int{}
		for _, p := range res.Pairs {
			received[p.Receiver]++
			given[p.Giver]++
		}
		limit := in.Cap
		if limit == 0 {
			limit = DefaultCap
		}
		for _, p := range in.Participants {
			if received[p.ID] != 1 {
				t.Fatalf("%d reçoit %d cadeaux", p.ID, received[p.ID])
			}
			switch roleOf(p) {
			case RoleReceptionOnly:
				if given[p.ID] != 0 {
					t.Fatalf("la réception seule %d offre %d cadeaux", p.ID, given[p.ID])
				}
			case RoleOneGift:
				if given[p.ID] != 1 {
					t.Fatalf("le un_seul %d offre %d cadeaux", p.ID, given[p.ID])
				}
			default:
				if given[p.ID] < 1 || given[p.ID] > limit {
					t.Fatalf("%d offre %d cadeaux (plafond %d)", p.ID, given[p.ID], limit)
				}
			}
		}
		if err := o.verify(res); err != nil {
			t.Fatal(err)
		}
	})
}
