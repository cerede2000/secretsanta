package adjust

import (
	"testing"
)

// F4c.AC2 : W est inséré dans une paire A→B qui devient A→W et W→B, en
// choisissant un A qui n'a pas encore découvert sa cible.
func TestAddInsertsIntoUndiscoveredPair(t *testing.T) {
	e := newEnv(t)
	gid := e.group(t, "famille", 2)
	ids := e.people(t, gid, normal, normal, normal, normal)
	a, b, c, d := ids[0], ids[1], ids[2], ids[3]
	before := ring(a, b, c, d)
	e.setPairs(t, gid, before)
	// Tous ont découvert leur cible, sauf c (c→d).
	e.discover(t, gid, a, b)
	e.discover(t, gid, b, c)
	e.discover(t, gid, d, a)
	w := e.add(t, gid, "Nouveau", normal)

	p := e.preview(t, gid, addP(w))
	if !p.Possible || p.AlreadyDiscovered != 0 {
		t.Fatalf("aperçu inattendu : %+v", p)
	}
	e.apply(t, gid, addP(w))
	samePairSet(t, e.pairs(t, gid), []Pair{{a, b}, {b, c}, {c, w}, {w, d}, {d, a}})
}

// F4c.AC2 : sans tireur qui n'a pas découvert sa cible, l'insertion se fait
// quand même dans une seule paire (A→W, W→B).
func TestAddInsertsIntoOnePairWhenAllDiscovered(t *testing.T) {
	e := newEnv(t)
	gid := e.group(t, "famille", 2)
	ids := e.people(t, gid, normal, normal, normal)
	before := ring(ids...)
	e.setPairs(t, gid, before)
	for _, q := range before {
		e.discover(t, gid, q.Giver, q.Receiver)
	}
	w := e.add(t, gid, "Nouveau", normal)

	p := e.preview(t, gid, addP(w))
	if p.Affected != 1 || p.AlreadyDiscovered != 1 || p.ChangedPairs != 2 {
		t.Fatalf("aperçu inattendu : %+v", p)
	}
	e.apply(t, gid, addP(w))
	got := e.pairs(t, gid)
	if n := changedCount(before, got); n != 2 {
		t.Fatalf("%d paires nouvelles, attendu 2", n)
	}
	// La paire coupée A→B devient A→W et W→B.
	var cut Pair
	for _, q := range before {
		if !contains(got, q) {
			cut = q
		}
	}
	if !contains(got, Pair{cut.Giver, w}) || !contains(got, Pair{w, cut.Receiver}) {
		t.Fatalf("insertion attendue dans %v : %v", cut, got)
	}
}

// Ajouter une personne déjà dans le tirage est refusé.
func TestAddRefusesPersonAlreadyInDraw(t *testing.T) {
	e := newEnv(t)
	gid := e.group(t, "famille", 2)
	ids := e.people(t, gid, normal, normal, normal)
	e.setPairs(t, gid, ring(ids...))
	_, err := e.svc.Preview(ctx, admin(gid), gid, addP(ids[0]))
	wantKind(t, err, "conflict")
}

func contains(ps []Pair, p Pair) bool {
	for _, q := range ps {
		if q == p {
			return true
		}
	}
	return false
}
