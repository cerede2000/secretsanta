package drawengine

import "testing"

// F4.AC13 : avec une personne reception_seule et deux offre_plus (n ≤ 6),
// les affectations valides sont énumérées, puis un χ² sur 10 000 tirages ne
// rejette pas l'uniformité (α = 0,001).
func TestUniformityWithCapacitiesChi2(t *testing.T) {
	if testing.Short() {
		t.Skip("test statistique long")
	}
	cases := map[string]Input{
		"n = 5": {Participants: people(RoleReceptionOnly, RoleGivesMore, RoleGivesMore, RoleNormal, RoleNormal)},
		"n = 6 avec un foyer": {Participants: []Participant{
			{ID: 1001, Role: RoleReceptionOnly},
			{ID: 1002, Role: RoleGivesMore, Household: "maison-a"},
			{ID: 1003, Role: RoleGivesMore},
			{ID: 1004, Role: RoleNormal, Household: "maison-a"},
			{ID: 1005, Role: RoleNormal},
			{ID: 1006, Role: RoleOneGift},
		}},
	}
	for name, in := range cases {
		t.Run(name, func(t *testing.T) {
			// L'énumération confirme la structure : un seul des deux offre_plus
			// offre deux cadeaux, tous les autres tireurs un seul.
			o := newOracle(in)
			valid := o.enumerate(0)
			if len(valid) == 0 {
				t.Fatal("aucune affectation valide énumérée")
			}
			for _, a := range valid {
				deg := map[int]int{}
				for _, g := range a {
					deg[g]++
				}
				if deg[0] != 0 || deg[1]+deg[2] != 3 || deg[1] < 1 || deg[2] < 1 {
					t.Fatalf("affectation énumérée hors des règles : %v", a)
				}
			}
			stat, critical, cells := chiSquareUniform(t, in, 10000)
			t.Logf("%d affectations valides, χ² = %.1f, seuil = %.1f", cells, stat, critical)
			if stat > critical {
				t.Fatalf("uniformité rejetée : χ² = %.1f > %.1f (%d cases)", stat, critical, cells)
			}
		})
	}
}
