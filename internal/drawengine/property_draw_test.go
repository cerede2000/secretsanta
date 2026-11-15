package drawengine

import (
	"testing"

	"pgregory.net/rapid"
)

// F4.AC2 : pour toute instance générée avec au moins une solution, le tirage
// n'échoue jamais et respecte exclusions, forçages, absence
// d'auto-attribution, option anti-échange et capacités.
func TestPropertyDrawNeverFailsWhenSolutionExists(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		in := genInput(t, genOpts{minN: 2, maxN: 7, roles: true, constraints: true, capChoices: []int{0, 2, 3}})
		fallback := rapid.Bool().Draw(t, "recherche_exhaustive")
		opts := DefaultOptions()
		if fallback {
			opts.MaxAttempts = 0
		}
		o := newOracle(in)
		solutions := o.enumerate(1)
		res, err := DrawWithOptions(in, opts)
		if len(solutions) == 0 {
			if err == nil {
				t.Fatalf("tirage accepté sur une instance sans solution : %v", res.Pairs)
			}
			asInfeasible(t, err)
			return
		}
		if err != nil {
			t.Fatalf("tirage refusé alors qu'une solution existe : %v", err)
		}
		if err := o.verify(res); err != nil {
			t.Fatalf("tirage invalide : %v", err)
		}
		if fallback && !res.Fallback {
			t.Fatal("la recherche exhaustive aurait dû produire le résultat")
		}
		for _, p := range res.Pairs {
			if p.Giver == p.Receiver {
				t.Fatalf("auto-attribution %v", p)
			}
		}
	})
}

// Sans rôles ni plafond, les mêmes propriétés tiennent sur des permutations.
func TestPropertyDrawPermutations(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		in := genInput(t, genOpts{minN: 2, maxN: 7, constraints: true})
		o := newOracle(in)
		solutions := o.enumerate(1)
		res, err := Draw(in)
		if len(solutions) == 0 {
			if err == nil {
				t.Fatal("tirage accepté sans solution")
			}
			return
		}
		if err != nil {
			t.Fatalf("tirage refusé alors qu'une solution existe : %v", err)
		}
		if err := o.verify(res); err != nil {
			t.Fatal(err)
		}
		for id, d := range o.degrees(res) {
			if d != 1 {
				t.Fatalf("%d offre %d cadeaux dans une permutation", id, d)
			}
		}
	})
}
