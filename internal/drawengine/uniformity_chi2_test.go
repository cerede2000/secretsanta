package drawengine

import "testing"

// F4.AC3 : le test du χ² sur 10 000 tirages à n = 5 ne rejette pas
// l'uniformité des permutations valides (α = 0,001).
func TestUniformityChi2FivePeople(t *testing.T) {
	if testing.Short() {
		t.Skip("test statistique long")
	}
	cases := map[string]Input{
		"sans contrainte": {Participants: normals(5)},
		"avec un foyer et une exclusion": {
			Participants: []Participant{
				{ID: 1001, Household: "maison-a"}, {ID: 1002, Household: "maison-a"},
				{ID: 1003}, {ID: 1004}, {ID: 1005},
			},
			Exclusions: []Pair{{1003, 1004}},
		},
	}
	for name, in := range cases {
		t.Run(name, func(t *testing.T) {
			stat, critical, cells := chiSquareUniform(t, in, 10000)
			t.Logf("%d permutations valides, χ² = %.1f, seuil = %.1f", cells, stat, critical)
			if stat > critical {
				t.Fatalf("uniformité rejetée : χ² = %.1f > %.1f (%d cases)", stat, critical, cells)
			}
		})
	}
}

// Le seuil du χ² approché reste proche des valeurs tabulées.
func TestChiSquareCriticalApproximation(t *testing.T) {
	// Tables : χ²(0,999 ; 43) ≈ 77,42 ; χ²(0,999 ; 100) ≈ 149,45.
	for df, want := range map[int]float64{43: 77.42, 100: 149.45} {
		got := chiSquareCritical(df, zAlpha001)
		if got < want-1 || got > want+1 {
			t.Fatalf("df=%d : %.2f, attendu ≈ %.2f", df, got, want)
		}
	}
}
