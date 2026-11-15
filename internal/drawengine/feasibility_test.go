package drawengine

import (
	"testing"

	"pgregory.net/rapid"
)

// F4.AC1 : le contrôle de faisabilité détecte toute instance sans solution et
// explique la cause sans nommer de paire.
func TestPropertyCheckDetectsEveryInfeasibleInstance(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		in := genInput(t, genOpts{minN: 1, maxN: 7, roles: true, constraints: true, capChoices: []int{0, 1, 2, 3}})
		exists := len(newOracle(in).enumerate(1)) > 0
		err := Check(in)
		if exists && err != nil {
			t.Fatalf("instance faisable refusée : %v", err)
		}
		if !exists {
			if err == nil {
				t.Fatal("instance sans solution non détectée")
			}
			asInfeasible(t, err)
			assertNoIdentity(t, in, err)
		}
	})
}

// Le flot détecte une violation de Hall que les contrôles simples ne voient
// pas : trois personnes ne peuvent offrir qu'à la même et à une autre.
func TestFlowDetectsHallViolation(t *testing.T) {
	ps := normals(5)
	in := Input{Participants: ps}
	// 1001, 1002 et 1003 ne peuvent offrir qu'à 1004 ou 1005 : chacun a un
	// destinataire possible, mais trois tireurs pour deux destinataires.
	for _, g := range []int64{1001, 1002, 1003} {
		for _, r := range []int64{1001, 1002, 1003} {
			if g != r {
				in.Exclusions = append(in.Exclusions, Pair{g, r})
			}
		}
	}
	err := Check(in)
	inf := asInfeasible(t, err)
	if inf.Cause != CauseExclusions {
		t.Fatalf("cause %q, attendu %q", inf.Cause, CauseExclusions)
	}
	assertNoIdentity(t, in, err)
	if _, err := Draw(in); err == nil {
		t.Fatal("le tirage aurait dû être refusé")
	}
}

func TestFeasibilityCauses(t *testing.T) {
	cases := []struct {
		name  string
		in    Input
		cause Cause
	}{
		{
			name:  "personne sans destinataire",
			in:    Input{Participants: normals(4), Exclusions: []Pair{{1001, 1002}, {1001, 1003}, {1001, 1004}}},
			cause: CauseNoRecipient,
		},
		{
			name:  "personne sans donneur",
			in:    Input{Participants: normals(4), Exclusions: []Pair{{1002, 1001}, {1003, 1001}, {1004, 1001}}},
			cause: CauseNoGiver,
		},
		{
			name:  "forçage contradictoire avec une exclusion",
			in:    Input{Participants: normals(4), Exclusions: []Pair{{1001, 1002}}, Forced: []Pair{{1001, 1002}}},
			cause: CauseForcedConflict,
		},
		{
			name:  "forçage vers soi-même",
			in:    Input{Participants: normals(4), Forced: []Pair{{1001, 1001}}},
			cause: CauseForcedSelf,
		},
		{
			name:  "deux forçages vers le même destinataire",
			in:    Input{Participants: normals(4), Forced: []Pair{{1001, 1003}, {1002, 1003}}},
			cause: CauseForcedTwice,
		},
		{
			name:  "forçages réciproques avec anti-échange",
			in:    Input{Participants: normals(4), AntiSwap: true, Forced: []Pair{{1001, 1002}, {1002, 1001}}},
			cause: CauseForcedSwap,
		},
		{
			name: "foyer trop grand",
			in: Input{Participants: []Participant{
				{ID: 1001, Household: "maison-x"}, {ID: 1002, Household: "maison-x"}, {ID: 1003, Household: "maison-x"},
				{ID: 1004}, {ID: 1005},
			}},
			cause: CauseHouseholdTooLarge,
		},
		{
			name: "forçages incompatibles avec les foyers",
			in: Input{
				Participants: []Participant{{ID: 1001, Household: "maison-x"}, {ID: 1002, Household: "maison-x"}, {ID: 1003}, {ID: 1004}},
				// 1003 et 1004 sont forcés entre eux : le foyer n'a plus personne
				// à qui offrir.
				Forced: []Pair{{1003, 1004}, {1004, 1003}},
			},
			cause: CauseForced,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := Check(c.in)
			inf := asInfeasible(t, err)
			if inf.Cause != c.cause {
				t.Fatalf("cause %q (%s), attendu %q", inf.Cause, inf.Message, c.cause)
			}
			assertNoIdentity(t, c.in, err)
			if _, err := Draw(c.in); err == nil {
				t.Fatal("le tirage aurait dû être refusé")
			}
		})
	}
}

func TestInvalidInputIsRefused(t *testing.T) {
	cases := map[string]Input{
		"aucun participant":     {},
		"identifiant en double": {Participants: []Participant{{ID: 1001}, {ID: 1001}, {ID: 1002}}},
		"rôle inconnu":          {Participants: []Participant{{ID: 1001, Role: "autre"}, {ID: 1002}}},
		"plafond négatif":       {Participants: normals(3), Cap: -1},
		"forçage inconnu":       {Participants: normals(3), Forced: []Pair{{1001, 9999}}},
	}
	for name, in := range cases {
		t.Run(name, func(t *testing.T) {
			if inf := asInfeasible(t, Check(in)); inf.Cause != CauseInvalidInput {
				t.Fatalf("cause %q", inf.Cause)
			}
		})
	}
}

// Le flot de Dinic sur un biparti à capacités 1 (Hopcroft-Karp) trouve un
// couplage maximum.
func TestDinicUnitBipartiteMatching(t *testing.T) {
	// Gauche 0..3, droite 4..7, source 8, puits 9.
	d := newDinic(10)
	edges := [][2]int{{0, 4}, {0, 5}, {1, 4}, {2, 5}, {2, 6}, {3, 6}}
	for _, e := range edges {
		d.addEdge(e[0], e[1], 1)
	}
	for i := 0; i < 4; i++ {
		d.addEdge(8, i, 1)
		d.addEdge(4+i, 9, 1)
	}
	// 7 n'a aucun voisin : le couplage maximum vaut 3.
	if got := d.maxFlow(8, 9); got != 3 {
		t.Fatalf("couplage %d, attendu 3", got)
	}
}
