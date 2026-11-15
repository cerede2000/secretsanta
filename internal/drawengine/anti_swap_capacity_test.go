package drawengine

import "testing"

// F4.AC15 : l'option anti-échange interdit A→B et B→A même quand un tireur
// offre deux cadeaux.

func TestAntiSwapWithTwoGiftGiver(t *testing.T) {
	in := Input{
		Participants: people(RoleReceptionOnly, RoleGivesMore, RoleNormal, RoleNormal, RoleNormal),
		AntiSwap:     true,
	}
	for _, opts := range []Options{DefaultOptions(), noAttempts()} {
		for i := 0; i < 300; i++ {
			res, err := DrawWithOptions(in, opts)
			if err != nil {
				t.Fatal(err)
			}
			if err := newOracle(in).verify(res); err != nil {
				t.Fatal(err)
			}
			gives := map[Pair]bool{}
			for _, p := range res.Pairs {
				gives[p] = true
			}
			for _, p := range res.Pairs {
				if gives[Pair{p.Receiver, p.Giver}] {
					t.Fatalf("échange réciproque %v", p)
				}
			}
			if newOracle(in).degrees(res)[1002] != 2 {
				t.Fatal("l'offre_plus devrait offrir deux cadeaux")
			}
		}
	}
}

// 1001 reçoit seulement, 1002 (offre_plus) offre deux cadeaux, 1003 un : la
// seule affectation est 1002→{1001, 1003} et 1003→1002, un échange.
func TestAntiSwapForbidsTheOnlySwapWithTwoGiftGiver(t *testing.T) {
	in := Input{Participants: people(RoleReceptionOnly, RoleGivesMore, RoleNormal)}
	res := mustDraw(t, in)
	if len(res.Pairs) != 3 {
		t.Fatalf("%v", res.Pairs)
	}
	in.AntiSwap = true
	_, err := Draw(in)
	if inf := asInfeasible(t, err); inf.Cause != CauseAntiSwap {
		t.Fatalf("cause %q", inf.Cause)
	}
	assertNoIdentity(t, in, err)
}
