package drawengine

import (
	"strings"
	"testing"
)

// F4.AC16 : un forçage consomme une capacité de son tireur ; un forçage dont
// le tireur est reception_seule est refusé avec explication.

func TestForcedPairConsumesOneGiftGiverCapacity(t *testing.T) {
	in := Input{
		Participants: people(RoleReceptionOnly, RoleGivesMore, RoleNormal, RoleOneGift, RoleNormal),
		Forced:       []Pair{{1004, 1001}},
	}
	for i := 0; i < 200; i++ {
		res := mustDraw(t, in)
		for _, p := range res.Pairs {
			if p.Giver == 1004 && p.Receiver != 1001 {
				t.Fatalf("le un_seul forcé offre aussi à %d", p.Receiver)
			}
		}
	}
}

func TestForcedPairsFillGivesMoreQuota(t *testing.T) {
	// L'offre_plus porte deux cadeaux ; ses deux forçages les consomment.
	in := Input{
		Participants: people(RoleReceptionOnly, RoleGivesMore, RoleNormal, RoleNormal),
		Forced:       []Pair{{1002, 1001}, {1002, 1003}},
	}
	for i := 0; i < 100; i++ {
		res := mustDraw(t, in)
		for _, p := range res.Pairs {
			if p.Giver == 1002 && p.Receiver != 1001 && p.Receiver != 1003 {
				t.Fatalf("l'offre_plus offre un troisième cadeau : %v", res.Pairs)
			}
		}
	}
}

func TestForcedPairsBeyondCapacityAreRefused(t *testing.T) {
	cases := map[string]Input{
		"un_seul avec deux forçages": {
			Participants: people(RoleOneGift, RoleNormal, RoleNormal, RoleNormal),
			Forced:       []Pair{{1001, 1002}, {1001, 1003}},
		},
		"normal avec deux forçages sans supplément": {
			Participants: normals(4),
			Forced:       []Pair{{1001, 1002}, {1001, 1003}},
		},
		"offre_plus avec trois forçages au plafond 2": {
			Participants: people(RoleGivesMore, RoleReceptionOnly, RoleReceptionOnly, RoleNormal, RoleNormal),
			Forced:       []Pair{{1001, 1002}, {1001, 1003}, {1001, 1004}},
		},
	}
	for name, in := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := Draw(in)
			if inf := asInfeasible(t, err); inf.Cause != CauseForcedOverCapacity {
				t.Fatalf("cause %q : %s", inf.Cause, inf.Message)
			}
			assertNoIdentity(t, in, err)
		})
	}
}

// Deux normal forcés chacun deux fois quand un seul supplément revient aux
// normal : chaque forçage tient seul, mais pas ensemble.
func TestForcedPairsExceedingTierTotalAreRefused(t *testing.T) {
	in := Input{
		Participants: people(RoleReceptionOnly, RoleNormal, RoleNormal, RoleNormal, RoleNormal),
		Forced:       []Pair{{1002, 1001}, {1002, 1003}, {1004, 1005}, {1004, 1002}},
	}
	_, err := Draw(in)
	if inf := asInfeasible(t, err); inf.Cause != CauseForced {
		t.Fatalf("cause %q : %s", inf.Cause, inf.Message)
	}
	assertNoIdentity(t, in, err)
}

func TestForcedGiverInReceptionOnlyIsRefused(t *testing.T) {
	in := Input{
		Participants: people(RoleReceptionOnly, RoleNormal, RoleNormal, RoleNormal),
		Forced:       []Pair{{1001, 1002}},
	}
	_, err := Draw(in)
	inf := asInfeasible(t, err)
	if inf.Cause != CauseForcedReceptionOnly || !strings.Contains(inf.Message, "réception seule") {
		t.Fatalf("refus %q : %s", inf.Cause, inf.Message)
	}
	assertNoIdentity(t, in, err)
}
