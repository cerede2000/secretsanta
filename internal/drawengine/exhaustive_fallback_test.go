package drawengine

import "testing"

func noAttempts() Options { return Options{MaxAttempts: 0} }

// isFixedShift dit si l'affectation est un décalage i → i+k (mod n) dans
// l'ordre des participants.
func isFixedShift(assign []int) bool {
	n := len(assign)
	k := ((0-assign[0])%n + n) % n // assign[0] est le tireur de 0
	for r, g := range assign {
		if (g+k)%n != r {
			return false
		}
	}
	return true
}

// F4.AC4 : au-delà du plafond d'essais, la recherche exhaustive trouve une
// solution quand elle existe, et le résultat n'est jamais un décalage fixe.
func TestExhaustiveFallbackIsRandomNotAFixedShift(t *testing.T) {
	in := Input{Participants: normals(6)}
	o := newOracle(in)
	distinct := map[string]bool{}
	shifts := 0
	const draws = 300
	for i := 0; i < draws; i++ {
		res, err := DrawWithOptions(in, noAttempts())
		if err != nil {
			t.Fatal(err)
		}
		if !res.Fallback {
			t.Fatal("la recherche exhaustive n'a pas été utilisée")
		}
		if err := o.verify(res); err != nil {
			t.Fatal(err)
		}
		a, _ := o.toAssign(res)
		distinct[assignKey(a)] = true
		if isFixedShift(a) {
			shifts++
		}
	}
	// 265 dérangements de 6 éléments, dont 5 décalages : un résultat
	// prévisible donnerait une ou cinq affectations distinctes.
	if len(distinct) < 50 {
		t.Fatalf("seulement %d affectations distinctes sur %d tirages", len(distinct), draws)
	}
	if shifts > draws/4 {
		t.Fatalf("%d décalages fixes sur %d tirages", shifts, draws)
	}
}

func TestIsFixedShiftHelper(t *testing.T) {
	if !isFixedShift([]int{3, 0, 1, 2}) || isFixedShift([]int{1, 0, 3, 2}) {
		t.Fatal("isFixedShift")
	}
}

// Une instance à solution unique, qui n'est pas un décalage : la recherche
// la trouve, et le tirage par défaut aussi (les essais aléatoires échouent
// presque toujours).
func TestExhaustiveFallbackFindsUniqueSolution(t *testing.T) {
	ps := normals(8)
	// Solution unique attendue : 1001→1003→1002→1005→1008→1004→1007→1006→1001.
	cycle := []int64{1001, 1003, 1002, 1005, 1008, 1004, 1007, 1006}
	want := map[int64]int64{}
	for i, g := range cycle {
		want[g] = cycle[(i+1)%len(cycle)]
	}
	in := Input{Participants: ps}
	for _, g := range ps {
		for _, r := range ps {
			if g.ID != r.ID && want[g.ID] != r.ID {
				in.Exclusions = append(in.Exclusions, Pair{g.ID, r.ID})
			}
		}
	}
	if got := len(newOracle(in).enumerate(0)); got != 1 {
		t.Fatalf("%d solutions, attendu 1", got)
	}
	for _, opts := range []Options{noAttempts(), DefaultOptions()} {
		res, err := DrawWithOptions(in, opts)
		if err != nil {
			t.Fatal(err)
		}
		for _, p := range res.Pairs {
			if want[p.Giver] != p.Receiver {
				t.Fatalf("paire inattendue %v", p)
			}
		}
	}
}

// Avec l'anti-échange, la recherche peut devoir revenir en arrière ; elle
// trouve tout de même une solution quand il en existe.
func TestExhaustiveFallbackWithAntiSwapAndCapacities(t *testing.T) {
	in := Input{
		Participants: people(RoleReceptionOnly, RoleGivesMore, RoleNormal, RoleNormal, RoleOneGift),
		AntiSwap:     true,
	}
	o := newOracle(in)
	if len(o.enumerate(1)) == 0 {
		t.Fatal("l'instance devrait avoir une solution")
	}
	for i := 0; i < 200; i++ {
		res, err := DrawWithOptions(in, noAttempts())
		if err != nil {
			t.Fatal(err)
		}
		if err := o.verify(res); err != nil {
			t.Fatal(err)
		}
	}
}

// Quand aucune solution n'existe, la recherche l'établit et le refus est
// expliqué.
func TestExhaustiveFallbackProvesImpossibility(t *testing.T) {
	in := Input{Participants: normals(2), AntiSwap: true}
	_, err := DrawWithOptions(in, noAttempts())
	if inf := asInfeasible(t, err); inf.Cause != CauseAntiSwap {
		t.Fatalf("cause %q", inf.Cause)
	}
}
