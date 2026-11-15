package drawengine

import (
	"strings"
	"testing"
)

// F4.AC5 : cas limites — 2 personnes, un seul foyer, exclusions
// pluriannuelles rendant le tirage impossible avec message explicatif.

func TestTwoPeopleGiveToEachOther(t *testing.T) {
	in := Input{Participants: normals(2)}
	for i := 0; i < 20; i++ {
		res := mustDraw(t, in)
		want := []Pair{{1001, 1002}, {1002, 1001}}
		if len(res.Pairs) != 2 || res.Pairs[0] != want[0] || res.Pairs[1] != want[1] {
			t.Fatalf("affectation %v", res.Pairs)
		}
	}
}

func TestTwoPeopleWithAntiSwapIsImpossible(t *testing.T) {
	in := Input{Participants: normals(2), AntiSwap: true}
	_, err := Draw(in)
	inf := asInfeasible(t, err)
	if inf.Cause != CauseAntiSwap || !strings.Contains(inf.Message, "anti-échange") {
		t.Fatalf("refus %q : %s", inf.Cause, inf.Message)
	}
	assertNoIdentity(t, in, err)
}

func TestSinglePersonIsImpossible(t *testing.T) {
	in := Input{Participants: normals(1)}
	_, err := Draw(in)
	if inf := asInfeasible(t, err); inf.Cause != CauseTooFewGivers {
		t.Fatalf("cause %q", inf.Cause)
	}
}

func TestSingleHouseholdIsImpossible(t *testing.T) {
	for _, n := range []int{2, 5} {
		ps := normals(n)
		for i := range ps {
			ps[i].Household = "maison-unique"
		}
		in := Input{Participants: ps}
		_, err := Draw(in)
		inf := asInfeasible(t, err)
		if inf.Cause != CauseHouseholdTooLarge || !strings.Contains(inf.Message, "foyer est trop grand") {
			t.Fatalf("n=%d : refus %q : %s", n, inf.Cause, inf.Message)
		}
		assertNoIdentity(t, in, err)
	}
}

func TestHouseholdOfHalfIsPossible(t *testing.T) {
	ps := normals(4)
	ps[0].Household, ps[1].Household = "maison-a", "maison-a"
	ps[2].Household, ps[3].Household = "maison-b", "maison-b"
	in := Input{Participants: ps}
	for i := 0; i < 50; i++ {
		mustDraw(t, in)
	}
}

// Trois personnes : seuls deux cycles sont possibles ; l'historique de deux
// années les a déjà tous deux utilisés.
func TestMultiYearExclusionsMakeDrawImpossible(t *testing.T) {
	ps := normals(3)
	yearMinus1 := []Pair{{1001, 1002}, {1002, 1003}, {1003, 1001}}
	yearMinus2 := []Pair{{1001, 1003}, {1003, 1002}, {1002, 1001}}

	t.Run("historique de plusieurs années", func(t *testing.T) {
		in := Input{Participants: ps, Forbidden: append(append([]Pair{}, yearMinus1...), yearMinus2...)}
		_, err := Draw(in)
		inf := asInfeasible(t, err)
		if inf.Cause != CauseHistory || !strings.Contains(inf.Message, "années précédentes") {
			t.Fatalf("refus %q : %s", inf.Cause, inf.Message)
		}
		assertNoIdentity(t, in, err)
	})

	t.Run("année précédente et années antérieures", func(t *testing.T) {
		in := Input{Participants: ps, Forbidden: yearMinus2, PreviousYear: yearMinus1, ExcludePreviousYear: true}
		_, err := Draw(in)
		inf := asInfeasible(t, err)
		if inf.Cause != CauseHistory || !strings.Contains(inf.Message, "années précédentes") {
			t.Fatalf("refus %q : %s", inf.Cause, inf.Message)
		}
		assertNoIdentity(t, in, err)
	})

	t.Run("une seule année d'historique laisse un tirage", func(t *testing.T) {
		in := Input{Participants: ps, Forbidden: yearMinus2}
		res := mustDraw(t, in)
		for _, p := range res.Pairs {
			for _, f := range yearMinus2 {
				if p == f {
					t.Fatalf("paire interdite tirée %v", p)
				}
			}
		}
	})
}

// Les paires d'historique qui citent une personne partie sont sans effet.
func TestHistoryAboutAbsentPeopleIsIgnored(t *testing.T) {
	in := Input{Participants: normals(3), Forbidden: []Pair{{1001, 9999}, {9999, 1002}}}
	mustDraw(t, in)
}
