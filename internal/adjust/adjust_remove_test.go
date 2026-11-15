package adjust

import (
	"testing"

	"secretsanta/internal/drawengine"
	"secretsanta/internal/participants"
)

const normal = participants.RoleNormal

// F4c.AC1 : retirer X fait offrir à Z, qui offrait à X, la cible Y de X ;
// une seule paire change et le reste du tirage est intact.
func TestRemoveReroutesGiverToRemovedTarget(t *testing.T) {
	e := newEnv(t)
	gid := e.group(t, "famille", 2)
	ids := e.people(t, gid, normal, normal, normal, normal, normal)
	a, b, c, d, x := ids[0], ids[1], ids[2], ids[3], ids[4]
	before := ring(a, b, c, d, x)
	e.setPairs(t, gid, before)

	// Z = d offre à X ; X offre à Y = a.
	e.apply(t, gid, remove(x))

	got := e.pairs(t, gid)
	samePairSet(t, got, []Pair{{a, b}, {b, c}, {c, d}, {d, a}})
	if n := changedCount(before, got); n != 1 {
		t.Fatalf("%d paires changées, attendu 1", n)
	}
	if n := e.count(t, `SELECT COUNT(*) FROM participants WHERE id = ?`, x); n != 0 {
		t.Fatal("la personne retirée est toujours dans le groupe")
	}
	if len(e.remover.calls) != 1 || e.remover.calls[0] != x {
		t.Fatalf("retrait reçu : %v", e.remover.calls)
	}
}

// F4c.AC1 : quand la paire Z→Y est interdite, la réparation respecte les
// mêmes contraintes et change un nombre minimal de paires (comparé à
// l'énumération exhaustive).
func TestRemoveRepairsMinimallyWhenRerouteForbidden(t *testing.T) {
	e := newEnv(t)
	gid := e.group(t, "famille", 2)
	ids := e.people(t, gid, normal, normal, normal, normal, normal, normal)
	a, b, c, d, f, x := ids[0], ids[1], ids[2], ids[3], ids[4], ids[5]
	before := ring(a, b, c, x, d, f)
	e.setPairs(t, gid, before)
	// Z = c, Y = d : la paire c→d est désormais interdite.
	if _, err := e.rules.AddExclusion(ctx, gid, c, d, false); err != nil {
		t.Fatal(err)
	}
	if err := e.rules.AddForced(ctx, gid, f, a); err != nil {
		t.Fatal(err)
	}

	p := e.preview(t, gid, remove(x))
	if !p.Possible {
		t.Fatalf("réparation attendue : %s", p.Summary)
	}
	e.apply(t, gid, remove(x))
	got := e.pairs(t, gid)

	roles := map[int64]participants.DrawRole{a: normal, b: normal, c: normal, d: normal, f: normal}
	checkValid(t, got, roles, 2)
	for _, q := range got {
		if q == (Pair{c, d}) {
			t.Fatal("la paire interdite a été créée")
		}
	}
	if len(receiversOf(got, f)) != 1 || receiversOf(got, f)[0] != a {
		t.Fatal("le forçage n'est pas respecté")
	}
	// Optimum exhaustif sur la même instance.
	prob := Problem{Cap: 2, Exclusions: []Pair{{c, d}}, Forced: []Pair{{f, a}}}
	for id := range roles {
		prob.People = append(prob.People, Person{ID: id, Role: drawengine.RoleNormal})
	}
	for _, q := range before {
		prob.Current = append(prob.Current, CurrentPair{Giver: q.Giver, Receiver: q.Receiver})
	}
	best, ok := bruteBest(prob)
	if !ok {
		t.Fatal("aucune solution énumérée")
	}
	if n := changedCount(before, got); n != best.changed {
		t.Fatalf("%d paires changées, minimum %d", n, best.changed)
	}
	if best.changed < 2 {
		t.Fatalf("le scénario doit exiger une vraie réparation (minimum %d)", best.changed)
	}
	if p.ChangedPairs != best.changed {
		t.Fatalf("aperçu : %d paires changées, attendu %d", p.ChangedPairs, best.changed)
	}
}

// Retirer une personne absente du tirage ou inconnue est refusé sans rien
// modifier.
func TestRemoveRefusesUnknownPerson(t *testing.T) {
	e := newEnv(t)
	gid := e.group(t, "famille", 2)
	ids := e.people(t, gid, normal, normal, normal)
	e.setPairs(t, gid, ring(ids...))
	late := e.add(t, gid, "Tardif", normal)
	before := e.pairs(t, gid)

	_, err := e.svc.Preview(ctx, admin(gid), gid, remove(late))
	wantKind(t, err, "conflict")
	_, err = e.svc.Preview(ctx, admin(gid), gid, remove(9999))
	wantKind(t, err, "not_found")
	samePairSet(t, e.pairs(t, gid), before)
}

// Seul l'administrateur du groupe ajuste, et seulement après confirmation de
// l'aperçu courant.
func TestAdjustRequiresAdminAndConfirmation(t *testing.T) {
	e := newEnv(t)
	gid := e.group(t, "famille", 2)
	other := e.group(t, "autre", 2)
	ids := e.people(t, gid, normal, normal, normal, normal)
	e.setPairs(t, gid, ring(ids...))
	before := e.pairs(t, gid)

	_, err := e.svc.Preview(ctx, participants.Actor{}, gid, remove(ids[0]))
	wantKind(t, err, "unauthorized")
	_, err = e.svc.Preview(ctx, participants.ParticipantActor(gid, ids[1]), gid, remove(ids[0]))
	wantKind(t, err, "forbidden")
	_, err = e.svc.Apply(ctx, admin(other), gid, remove(ids[0]), Confirmation{Confirmed: true})
	wantKind(t, err, "forbidden")

	p := e.preview(t, gid, remove(ids[0]))
	_, err = e.svc.Apply(ctx, admin(gid), gid, remove(ids[0]), Confirmation{Revision: p.Revision})
	wantKind(t, err, "validation")
	_, err = e.svc.Apply(ctx, admin(gid), gid, remove(ids[0]), Confirmation{Confirmed: true, Revision: p.Revision - 1})
	wantKind(t, err, "conflict")
	samePairSet(t, e.pairs(t, gid), before)
	if len(e.mail.Sent) != 0 || len(e.archiver.calls) != 0 {
		t.Fatal("rien ne doit partir sans confirmation")
	}
}
