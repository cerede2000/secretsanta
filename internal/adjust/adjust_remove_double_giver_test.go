package adjust

import (
	"testing"

	"secretsanta/internal/participants"
)

const (
	offrePlus = participants.RoleOffrePlus
	unSeul    = participants.RoleUnSeul
	reception = participants.RoleReceptionSeule
)

// F4c.AC9 : le retrait d'un offre_plus qui offrait deux cadeaux reroute ses
// deux cibles avec deux paires nouvelles seulement ; le cadeau
// supplémentaire va à un autre offre_plus, jamais à un un_seul ni à un
// normal quand un offre_plus a de la place.
func TestRemoveDoubleGiverReroutesToOffrePlus(t *testing.T) {
	e := newEnv(t)
	gid := e.group(t, "famille", 2)
	// z, x(op), a, b, o(op), m, u(un_seul), r(réception seule)
	ids := e.people(t, gid, normal, offrePlus, normal, normal, offrePlus, normal, unSeul, reception)
	z, x, a, b, o, m, u, r := ids[0], ids[1], ids[2], ids[3], ids[4], ids[5], ids[6], ids[7]
	before := []Pair{{z, x}, {x, a}, {x, r}, {a, b}, {b, o}, {o, m}, {m, u}, {u, z}}
	e.setPairs(t, gid, before)

	p := e.preview(t, gid, remove(x))
	if p.ChangedPairs != 2 {
		t.Fatalf("aperçu : %d paires nouvelles, attendu 2", p.ChangedPairs)
	}
	e.apply(t, gid, remove(x))
	got := e.pairs(t, gid)

	roles := map[int64]participants.DrawRole{z: normal, a: normal, b: normal, o: offrePlus, m: normal, u: unSeul, r: reception}
	checkValid(t, got, roles, 2)
	if n := changedCount(before, got); n != 2 {
		t.Fatalf("%d paires changées, attendu 2", n)
	}
	if len(receiversOf(got, o)) != 2 {
		t.Fatalf("l'offre_plus restant doit porter le cadeau supplémentaire : %v", got)
	}
	if len(receiversOf(got, z)) != 1 || len(receiversOf(got, u)) != 1 {
		t.Fatalf("le cadeau supplémentaire ne doit aller ni au normal ni au un_seul : %v", got)
	}
	for _, keep := range []Pair{{a, b}, {b, o}, {o, m}, {m, u}, {u, z}} {
		if !contains(got, keep) {
			t.Fatalf("paire %v changée sans nécessité", keep)
		}
	}
}

// F4c.AC9 : sans autre offre_plus disponible, le cadeau supplémentaire va à
// un normal, jamais au un_seul, toujours avec deux paires nouvelles.
func TestRemoveDoubleGiverNeverOverloadsUnSeul(t *testing.T) {
	e := newEnv(t)
	gid := e.group(t, "famille", 2)
	// z(un_seul), x(op), a(un_seul), b(normal), r(réception)
	ids := e.people(t, gid, unSeul, offrePlus, unSeul, normal, reception)
	z, x, a, b, r := ids[0], ids[1], ids[2], ids[3], ids[4]
	before := []Pair{{z, x}, {x, a}, {x, r}, {a, b}, {b, z}}
	e.setPairs(t, gid, before)

	e.apply(t, gid, remove(x))
	got := e.pairs(t, gid)
	roles := map[int64]participants.DrawRole{z: unSeul, a: unSeul, b: normal, r: reception}
	checkValid(t, got, roles, 2)
	if n := changedCount(before, got); n != 2 {
		t.Fatalf("%d paires changées, attendu 2 : %v", n, got)
	}
	if len(receiversOf(got, b)) != 2 {
		t.Fatalf("le normal doit porter le cadeau supplémentaire : %v", got)
	}
}
