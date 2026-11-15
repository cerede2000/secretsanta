package drawengine

import (
	"strings"
	"testing"
)

// F4.AC11 : capacité insuffisante → refus sans aucun nom, avec la cause, le
// nombre manquant et les trois propositions.

func assertCapacityRefusal(t *testing.T, in Input, cause Cause, reason, missing string, n int) {
	t.Helper()
	_, err := Draw(in)
	inf := asInfeasible(t, err)
	if inf.Cause != cause {
		t.Fatalf("cause %q, attendu %q (%s)", inf.Cause, cause, inf.Message)
	}
	if !strings.Contains(inf.Message, reason) {
		t.Fatalf("message sans la cause %q : %s", reason, inf.Message)
	}
	if !strings.Contains(inf.Message, missing) || inf.Missing != n {
		t.Fatalf("manque %d attendu (%q) : %s / %d", n, missing, inf.Message, inf.Missing)
	}
	if len(inf.Suggestions) != 3 {
		t.Fatalf("%d propositions, attendu 3", len(inf.Suggestions))
	}
	full := err.Error()
	for _, want := range []string{"« J'offre plus »", "« normal »", "Relever le plafond", "confirmation explicite"} {
		if !strings.Contains(full, want) {
			t.Fatalf("proposition %q absente : %s", want, full)
		}
	}
	assertNoIdentity(t, in, err)
	if err := Check(in); err == nil {
		t.Fatal("Check aurait dû refuser")
	}
}

func TestCapacityRefusalTooManyOneGift(t *testing.T) {
	// R = 2 ; seul 1003 peut porter un supplément (plafond 2) : il en manque 1.
	in := Input{Participants: people(RoleReceptionOnly, RoleReceptionOnly, RoleNormal, RoleOneGift, RoleOneGift)}
	assertCapacityRefusal(t, in, CauseTooManyOneGift,
		"trop de personnes ne peuvent pas ou ne veulent pas offrir plus d'un cadeau", "il manque 1 cadeau.", 1)
}

func TestCapacityRefusalOnlyOneGiftGivers(t *testing.T) {
	in := Input{Participants: people(RoleReceptionOnly, RoleReceptionOnly, RoleOneGift, RoleOneGift, RoleOneGift)}
	assertCapacityRefusal(t, in, CauseTooManyOneGift,
		"trop de personnes ne peuvent pas ou ne veulent pas offrir plus d'un cadeau", "il manque 2 cadeaux", 2)
}

func TestCapacityRefusalCapTooLow(t *testing.T) {
	// Deux tireurs normal, trois réceptions seules : capacité 2 au plafond 2.
	in := Input{Participants: people(RoleNormal, RoleNormal, RoleReceptionOnly, RoleReceptionOnly, RoleReceptionOnly)}
	assertCapacityRefusal(t, in, CauseCapTooLow, "le plafond est trop bas", "il manque 1 cadeau.", 1)
}

func TestCapacityRefusalCapOne(t *testing.T) {
	in := Input{Participants: people(RoleNormal, RoleGivesMore, RoleNormal, RoleReceptionOnly, RoleReceptionOnly), Cap: 1}
	assertCapacityRefusal(t, in, CauseCapTooLow, "le plafond est trop bas", "il manque 2 cadeaux", 2)
}
