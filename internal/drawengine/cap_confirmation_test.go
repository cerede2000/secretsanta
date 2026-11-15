package drawengine

import (
	"strings"
	"testing"
)

// F4.AC12 : un plafond supérieur à 2, ou une répartition qui l'exige, est
// refusé sans indicateur de confirmation explicite et accepté avec.

func TestCapAboveTwoNeedsConfirmation(t *testing.T) {
	in := Input{Participants: normals(4), Cap: 3}
	_, err := Draw(in)
	inf := asInfeasible(t, err)
	if inf.Cause != CauseCapNeedsConfirmation || !strings.Contains(inf.Message, "confirmé explicitement") {
		t.Fatalf("refus %q : %s", inf.Cause, inf.Message)
	}
	assertNoIdentity(t, in, err)
	if Check(in) == nil {
		t.Fatal("Check aurait dû refuser")
	}
	in.CapConfirmed = true
	mustDraw(t, in)
}

func TestCapTwoNeedsNoConfirmation(t *testing.T) {
	for _, c := range []int{0, 2} {
		mustDraw(t, Input{Participants: people(RoleReceptionOnly, RoleNormal, RoleNormal), Cap: c})
	}
}

// Deux tireurs pour trois réceptions seules : la répartition exige qu'un
// tireur offre 3 cadeaux.
func TestDistributionRequiringThreeNeedsConfirmation(t *testing.T) {
	in := Input{Participants: people(RoleNormal, RoleNormal, RoleReceptionOnly, RoleReceptionOnly, RoleReceptionOnly)}

	// Au plafond par défaut : refus, avec la proposition de relever le
	// plafond sous confirmation.
	_, err := Draw(in)
	if inf := asInfeasible(t, err); inf.Cause != CauseCapTooLow || !strings.Contains(err.Error(), "confirmation explicite") {
		t.Fatalf("refus %q : %v", inf.Cause, err)
	}

	// Plafond 3 sans confirmation : refus.
	in.Cap = 3
	if inf := asInfeasible(t, func() error { _, err := Draw(in); return err }()); inf.Cause != CauseCapNeedsConfirmation {
		t.Fatalf("cause %q", inf.Cause)
	}

	// Plafond 3 confirmé : accepté, un tireur offre 3 cadeaux, l'autre 2.
	in.CapConfirmed = true
	for i := 0; i < 50; i++ {
		res := mustDraw(t, in)
		d := newOracle(in).degrees(res)
		if d[1001]+d[1002] != 5 || d[1001] < 2 || d[1002] < 2 {
			t.Fatalf("degrés %v", d)
		}
	}
}
