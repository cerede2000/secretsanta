package drawengine

import "testing"

// F4.AC9 : les cadeaux supplémentaires vont d'abord aux offre_plus, puis aux
// normal seulement si la capacité des offre_plus ne suffit pas, jamais à un
// un_seul ni à un reception_seule ; sans réglage et sans reception_seule le
// résultat est une permutation.

func TestExtrasGoToGivesMoreFirst(t *testing.T) {
	// 1001 reçoit seulement, 1002 offre plus, 1003-1005 normal, 1006 un seul.
	in := Input{Participants: people(RoleReceptionOnly, RoleGivesMore, RoleNormal, RoleNormal, RoleNormal, RoleOneGift)}
	for i := 0; i < 300; i++ {
		res := mustDraw(t, in)
		d := newOracle(in).degrees(res)
		if d[1001] != 0 || d[1002] != 2 || d[1003] != 1 || d[1004] != 1 || d[1005] != 1 || d[1006] != 1 {
			t.Fatalf("degrés %v", d)
		}
	}
}

func TestNormalsTakeExtrasOnlyWhenGivesMoreAreFull(t *testing.T) {
	// R = 3, un seul offre_plus au plafond 2 : il porte 1 supplément, deux
	// normal portent les 2 autres ; le un_seul jamais.
	in := Input{Participants: people(
		RoleReceptionOnly, RoleReceptionOnly, RoleReceptionOnly,
		RoleGivesMore, RoleNormal, RoleNormal, RoleNormal, RoleNormal, RoleOneGift,
	)}
	for i := 0; i < 300; i++ {
		res := mustDraw(t, in)
		d := newOracle(in).degrees(res)
		if d[1001]+d[1002]+d[1003] != 0 {
			t.Fatalf("une réception seule offre : %v", d)
		}
		if d[1004] != 2 {
			t.Fatalf("l'offre_plus n'est pas au plafond : %v", d)
		}
		twos := 0
		for _, id := range []int64{1005, 1006, 1007, 1008} {
			if d[id] == 2 {
				twos++
			}
		}
		if twos != 2 {
			t.Fatalf("%d normal à deux cadeaux, attendu 2 : %v", twos, d)
		}
		if d[1009] != 1 {
			t.Fatalf("le un_seul offre %d cadeaux", d[1009])
		}
	}
}

func TestNoSettingsNoReceptionIsAPermutation(t *testing.T) {
	for _, in := range []Input{
		{Participants: normals(6)},
		{Participants: []Participant{{ID: 1001}, {ID: 1002}, {ID: 1003}, {ID: 1004}}}, // rôle vide = normal
	} {
		for i := 0; i < 200; i++ {
			res := mustDraw(t, in)
			for id, d := range newOracle(in).degrees(res) {
				if d != 1 {
					t.Fatalf("%d offre %d cadeaux : pas une permutation", id, d)
				}
			}
		}
	}
}

// Sans réception seule, même des offre_plus n'offrent qu'un cadeau.
func TestGivesMoreWithoutReceptionIsAPermutation(t *testing.T) {
	in := Input{Participants: people(RoleGivesMore, RoleGivesMore, RoleNormal, RoleOneGift)}
	for i := 0; i < 100; i++ {
		res := mustDraw(t, in)
		for id, d := range newOracle(in).degrees(res) {
			if d != 1 {
				t.Fatalf("%d offre %d cadeaux", id, d)
			}
		}
	}
}
