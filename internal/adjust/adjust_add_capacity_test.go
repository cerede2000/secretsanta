package adjust

import (
	"testing"

	"secretsanta/internal/participants"
)

// rolesOf renvoie les rôles du tirage actuel lus en base.
func (e *env) rolesOf(t *testing.T, gid int64) map[int64]participants.DrawRole {
	t.Helper()
	ro, err := e.parts.Roster(ctx, gid)
	if err != nil {
		t.Fatal(err)
	}
	out := map[int64]participants.DrawRole{}
	for _, en := range ro.Entries {
		out[en.ID] = en.Role
	}
	return out
}

// F4c.AC11 : une réception seule ajoutée reçoit son cadeau d'un offre_plus
// qui a encore de la place, avant tout normal ; une seule paire nouvelle.
func TestAddReceptionGoesToOffrePlusFirst(t *testing.T) {
	e := newEnv(t)
	gid := e.group(t, "famille", 2)
	ids := e.people(t, gid, normal, normal, offrePlus, normal)
	a, b, o, c := ids[0], ids[1], ids[2], ids[3]
	before := ring(a, b, o, c)
	e.setPairs(t, gid, before)
	w := e.add(t, gid, "Recoit", reception)

	e.apply(t, gid, addP(w))
	got := e.pairs(t, gid)
	checkValid(t, got, e.rolesOf(t, gid), 2)
	if n := changedCount(before, got); n != 1 || !contains(got, Pair{o, w}) {
		t.Fatalf("l'offre_plus doit offrir à la réception seule : %v", got)
	}
	if len(receiversOf(got, w)) != 0 {
		t.Fatal("une réception seule n'offre rien")
	}
}

// F4c.AC11 : sans offre_plus disponible (plafond atteint), le cadeau
// supplémentaire va à un normal, jamais à un un_seul.
func TestAddReceptionFallsBackToNormal(t *testing.T) {
	e := newEnv(t)
	gid := e.group(t, "famille", 2)
	ids := e.people(t, gid, offrePlus, unSeul, normal, reception)
	o, u, n, r := ids[0], ids[1], ids[2], ids[3]
	before := []Pair{{o, u}, {o, r}, {u, n}, {n, o}}
	e.setPairs(t, gid, before)
	w := e.add(t, gid, "Recoit", reception)

	e.apply(t, gid, addP(w))
	got := e.pairs(t, gid)
	checkValid(t, got, e.rolesOf(t, gid), 2)
	if n2 := changedCount(before, got); n2 != 1 || !contains(got, Pair{n, w}) {
		t.Fatalf("le normal doit offrir à la nouvelle réception seule : %v", got)
	}
}

// F4c.AC11 : un un_seul, un normal ou un offre_plus ajouté est inséré dans
// une paire (deux paires nouvelles) et n'offre qu'un cadeau ; personne ne
// dépasse le plafond.
func TestAddGiverOfAnyRoleIsInserted(t *testing.T) {
	for _, role := range []participants.DrawRole{unSeul, normal, offrePlus} {
		t.Run(string(role), func(t *testing.T) {
			e := newEnv(t)
			gid := e.group(t, "famille", 2)
			ids := e.people(t, gid, normal, unSeul, offrePlus, normal)
			before := ring(ids...)
			e.setPairs(t, gid, before)
			w := e.add(t, gid, "Entrant", role)

			e.apply(t, gid, addP(w))
			got := e.pairs(t, gid)
			checkValid(t, got, e.rolesOf(t, gid), 2)
			if n := changedCount(before, got); n != 2 {
				t.Fatalf("%d paires nouvelles, attendu 2 : %v", n, got)
			}
			if len(receiversOf(got, w)) != 1 {
				t.Fatalf("la personne ajoutée doit offrir un cadeau : %v", got)
			}
		})
	}
}

// F4c.AC11 : avec un plafond de 3 confirmé, un offre_plus peut porter un
// troisième cadeau, mais jamais au-delà.
func TestAddRespectsRaisedCap(t *testing.T) {
	e := newEnv(t)
	gid := e.group(t, "famille", 3)
	ids := e.people(t, gid, offrePlus, normal, unSeul, reception, reception)
	o, a, u, r1, r2 := ids[0], ids[1], ids[2], ids[3], ids[4]
	before := []Pair{{o, a}, {o, r1}, {o, r2}, {a, u}, {u, o}}
	e.setPairs(t, gid, before)
	w := e.add(t, gid, "Recoit", reception)

	e.apply(t, gid, addP(w))
	got := e.pairs(t, gid)
	checkValid(t, got, e.rolesOf(t, gid), 3)
	if len(receiversOf(got, o)) > 3 {
		t.Fatalf("plafond dépassé : %v", got)
	}
	if !contains(got, Pair{a, w}) {
		t.Fatalf("le normal doit prendre le cadeau quand l'offre_plus est au plafond : %v", got)
	}
}
