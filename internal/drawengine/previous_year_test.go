package drawengine

import (
	"strings"
	"testing"
)

// F4.AC6 : le tirage exclut les paires de l'année précédente quand l'option
// est active.
func TestPreviousYearPairsExcludedWhenOptionActive(t *testing.T) {
	previous := []Pair{{1001, 1002}, {1002, 1003}, {1003, 1004}, {1004, 1001}}
	isPrevious := map[Pair]bool{}
	for _, p := range previous {
		isPrevious[p] = true
	}
	const draws = 500

	on := Input{Participants: normals(4), PreviousYear: previous, ExcludePreviousYear: true}
	for i := 0; i < draws; i++ {
		res := mustDraw(t, on)
		for _, p := range res.Pairs {
			if isPrevious[p] {
				t.Fatalf("paire de l'année précédente tirée : %v", p)
			}
		}
	}
	// Recherche exhaustive incluse.
	for i := 0; i < 50; i++ {
		res, err := DrawWithOptions(on, noAttempts())
		if err != nil {
			t.Fatal(err)
		}
		for _, p := range res.Pairs {
			if isPrevious[p] {
				t.Fatalf("paire de l'année précédente tirée par la recherche : %v", p)
			}
		}
	}

	// Option inactive : les mêmes paires redeviennent possibles.
	off := Input{Participants: normals(4), PreviousYear: previous}
	seen := false
	for i := 0; i < draws && !seen; i++ {
		res := mustDraw(t, off)
		for _, p := range res.Pairs {
			seen = seen || isPrevious[p]
		}
	}
	if !seen {
		t.Fatal("option inactive : aucune paire de l'année précédente en 500 tirages")
	}
}

func TestPreviousYearAloneMakingDrawImpossibleIsExplained(t *testing.T) {
	// À deux, la seule affectation est l'échange de l'an dernier.
	in := Input{Participants: normals(2), PreviousYear: []Pair{{1001, 1002}}, ExcludePreviousYear: true}
	_, err := Draw(in)
	inf := asInfeasible(t, err)
	if inf.Cause != CauseHistory || !strings.Contains(inf.Message, "année précédente") {
		t.Fatalf("refus %q : %s", inf.Cause, inf.Message)
	}
	assertNoIdentity(t, in, err)
	in.ExcludePreviousYear = false
	mustDraw(t, in)
}
