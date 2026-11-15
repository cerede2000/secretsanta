package drawengine

import "testing"

// F4.AC14 : cas limites avec capacités.

func TestCapacityEdgeOneGiver(t *testing.T) {
	cases := map[string]Input{
		"un seul participant":                 {Participants: people(RoleGivesMore)},
		"tous en réception seule sauf un":     {Participants: people(RoleReceptionOnly, RoleGivesMore, RoleReceptionOnly, RoleReceptionOnly)},
		"tous en réception seule":             {Participants: people(RoleReceptionOnly, RoleReceptionOnly, RoleReceptionOnly)},
		"un tireur au plafond confirmé élevé": {Participants: people(RoleNormal, RoleReceptionOnly, RoleReceptionOnly), Cap: 5, CapConfirmed: true},
	}
	for name, in := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := Draw(in)
			if inf := asInfeasible(t, err); inf.Cause != CauseTooFewGivers {
				t.Fatalf("cause %q", inf.Cause)
			}
			assertNoIdentity(t, in, err)
		})
	}
}

func TestCapacityEdgeNoGivesMoreNormalsSolicited(t *testing.T) {
	in := Input{Participants: people(RoleReceptionOnly, RoleNormal, RoleNormal, RoleNormal, RoleOneGift)}
	seen := map[int64]bool{}
	for i := 0; i < 200; i++ {
		res := mustDraw(t, in)
		d := newOracle(in).degrees(res)
		if d[1005] != 1 || d[1002]+d[1003]+d[1004] != 4 {
			t.Fatalf("degrés %v", d)
		}
		for _, id := range []int64{1002, 1003, 1004} {
			if d[id] == 2 {
				seen[id] = true
			}
		}
	}
	if len(seen) != 3 {
		t.Fatalf("tous les normal devraient pouvoir être sollicités : %v", seen)
	}
}

func TestCapacityEdgeGivesMoreJustEnough(t *testing.T) {
	in := Input{Participants: people(RoleReceptionOnly, RoleReceptionOnly, RoleGivesMore, RoleGivesMore, RoleNormal, RoleNormal)}
	for i := 0; i < 200; i++ {
		res := mustDraw(t, in)
		d := newOracle(in).degrees(res)
		if d[1003] != 2 || d[1004] != 2 || d[1005] != 1 || d[1006] != 1 {
			t.Fatalf("degrés %v", d)
		}
	}
}

func TestCapacityEdgeTooManyOneGift(t *testing.T) {
	in := Input{Participants: people(RoleReceptionOnly, RoleOneGift, RoleOneGift, RoleOneGift)}
	_, err := Draw(in)
	if inf := asInfeasible(t, err); inf.Cause != CauseTooManyOneGift || inf.Missing != 1 {
		t.Fatalf("refus %q, manque %d", inf.Cause, inf.Missing)
	}
}

func TestCapacityEdgeCapThreeConfirmed(t *testing.T) {
	// Un offre_plus au plafond 3 porte les deux suppléments.
	in := Input{
		Participants: people(RoleReceptionOnly, RoleReceptionOnly, RoleGivesMore, RoleNormal, RoleNormal),
		Cap:          3,
		CapConfirmed: true,
	}
	for i := 0; i < 200; i++ {
		res := mustDraw(t, in)
		d := newOracle(in).degrees(res)
		if d[1003] != 3 || d[1004] != 1 || d[1005] != 1 {
			t.Fatalf("degrés %v", d)
		}
	}
}
