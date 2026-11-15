package drawengine

import "testing"

// F4.AC10 : au sein d'un même niveau, les cadeaux supplémentaires sont
// répartis équitablement (floor ou ceil) puis attribués au hasard, sans
// dépasser plafond moins un par tireur.

// Plafond 3 confirmé, deux offre_plus et deux suppléments : la répartition
// équitable impose 1 + 1, jamais 2 + 0, bien que le plafond le permette.
func TestExtrasAreSplitEvenlyWithinTier(t *testing.T) {
	in := Input{
		Participants: people(RoleReceptionOnly, RoleReceptionOnly, RoleGivesMore, RoleGivesMore, RoleNormal),
		Cap:          3,
		CapConfirmed: true,
	}
	for i := 0; i < 300; i++ {
		res := mustDraw(t, in)
		d := newOracle(in).degrees(res)
		if d[1003] != 2 || d[1004] != 2 || d[1005] != 1 {
			t.Fatalf("répartition inéquitable : %v", d)
		}
	}
}

// Trois suppléments pour deux offre_plus au plafond 3 : l'un en offre 3,
// l'autre 2, et chacun des deux est celui qui en offre un de plus.
func TestRemainderGoesToRandomMemberOfTier(t *testing.T) {
	in := Input{
		Participants: people(RoleReceptionOnly, RoleReceptionOnly, RoleReceptionOnly, RoleGivesMore, RoleGivesMore, RoleNormal),
		Cap:          3,
		CapConfirmed: true,
	}
	const draws = 400
	extra := map[int64]int{}
	for i := 0; i < draws; i++ {
		res := mustDraw(t, in)
		d := newOracle(in).degrees(res)
		if d[1004]+d[1005] != 5 || d[1004] < 2 || d[1005] < 2 || d[1004] > 3 || d[1005] > 3 || d[1006] != 1 {
			t.Fatalf("répartition %v", d)
		}
		if d[1004] == 3 {
			extra[1004]++
		} else {
			extra[1005]++
		}
	}
	// Chacun est choisi environ une fois sur deux (écart-type 10).
	for _, id := range []int64{1004, 1005} {
		if extra[id] < draws/2-60 || extra[id] > draws/2+60 {
			t.Fatalf("choix du supplément déséquilibré : %v", extra)
		}
	}
}

// Au niveau normal : un offre_plus au plafond, puis deux suppléments pour
// quatre normal, choisis au hasard.
func TestNormalTierRemainderIsRandom(t *testing.T) {
	in := Input{Participants: people(
		RoleReceptionOnly, RoleReceptionOnly, RoleReceptionOnly,
		RoleGivesMore, RoleNormal, RoleNormal, RoleNormal, RoleNormal,
	)}
	const draws = 800
	chosen := map[int64]int{}
	for i := 0; i < draws; i++ {
		res := mustDraw(t, in)
		d := newOracle(in).degrees(res)
		for _, id := range []int64{1005, 1006, 1007, 1008} {
			if d[id] > 2 || d[id] < 1 {
				t.Fatalf("normal hors de [1, plafond] : %v", d)
			}
			if d[id] == 2 {
				chosen[id]++
			}
		}
	}
	// Chaque normal est choisi environ une fois sur deux (écart-type ≈ 14).
	for _, id := range []int64{1005, 1006, 1007, 1008} {
		if chosen[id] < draws/2-90 || chosen[id] > draws/2+90 {
			t.Fatalf("choix des normal déséquilibré : %v", chosen)
		}
	}
}
