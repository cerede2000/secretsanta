package adjust

import (
	"testing"
)

// keptRows vérifie que chaque paire conservée garde sa ligne et son drapeau
// (jamais réarmé) et que chaque paire nouvelle est à découvrir.
func keptRows(t *testing.T, before, after []row) {
	t.Helper()
	old := map[Pair]row{}
	for _, r := range before {
		old[Pair{r.Giver, r.Receiver}] = r
	}
	for _, r := range after {
		prev, kept := old[Pair{r.Giver, r.Receiver}]
		if !kept {
			if r.Discovered.Valid {
				t.Fatalf("une nouvelle cible (%d→%d) est marquée découverte", r.Giver, r.Receiver)
			}
			continue
		}
		if r != prev {
			t.Fatalf("paire conservée modifiée : %+v → %+v", prev, r)
		}
	}
}

// F4c.AC16 : à l'ajout, la paire coupée est celle d'une personne qui n'a pas
// encore découvert sa cible ; les drapeaux des autres restent posés.
func TestAddAvoidsDiscoveredTargets(t *testing.T) {
	e := newEnv(t)
	gid := e.group(t, "famille", 2)
	ids := e.people(t, gid, normal, normal, normal, normal, normal)
	a, b, c, d, f := ids[0], ids[1], ids[2], ids[3], ids[4]
	e.setPairs(t, gid, ring(a, b, c, d, f))
	for _, q := range []Pair{{a, b}, {b, c}, {c, d}, {d, f}} {
		e.discover(t, gid, q.Giver, q.Receiver)
	}
	before := e.rows(t, gid)
	w := e.add(t, gid, "Nouveau", normal)

	r := e.apply(t, gid, addP(w))
	if r.AlreadyDiscovered != 0 {
		t.Fatalf("aucune cible découverte ne devait changer : %+v", r)
	}
	samePairSet(t, e.pairs(t, gid), []Pair{{a, b}, {b, c}, {c, d}, {d, f}, {f, w}, {w, a}})
	keptRows(t, before, e.rows(t, gid))
	if len(e.mail.Sent) != 2 {
		t.Fatalf("f et w doivent être prévenus : %+v", e.mail.Sent)
	}
}

// F4c.AC16 : au retrait, la réparation préfère changer plus de paires non
// découvertes plutôt qu'une seule cible déjà découverte.
func TestRemovePrefersUndiscoveredEvenIfMoreChanges(t *testing.T) {
	e := newEnv(t)
	gid := e.group(t, "famille", 2)
	ids := e.people(t, gid, normal, normal, normal, normal, normal, normal)
	a, b, c, x, d, f := ids[0], ids[1], ids[2], ids[3], ids[4], ids[5]
	// a→b→c→x→d→f→a ; c→d est interdit, le simple report est impossible.
	before := ring(a, b, c, x, d, f)
	e.setPairs(t, gid, before)
	if _, err := e.rules.AddExclusion(ctx, gid, c, d, false); err != nil {
		t.Fatal(err)
	}
	// Sans découverte, deux paires nouvelles suffiraient (en coupant f→a).
	free := Problem{Cap: 2, Exclusions: []Pair{{c, d}}}
	for _, id := range []int64{a, b, c, d, f} {
		free.People = append(free.People, Person{ID: id})
	}
	for _, q := range before {
		free.Current = append(free.Current, CurrentPair{Giver: q.Giver, Receiver: q.Receiver})
	}
	best, _ := bruteBest(free)
	if best.changed != 2 {
		t.Fatalf("scénario : minimum sans découverte %d, attendu 2", best.changed)
	}
	// a→b et f→a sont découvertes : toute réparation à deux paires en coupe une.
	e.discover(t, gid, a, b)
	e.discover(t, gid, f, a)
	rowsBefore := e.rows(t, gid)

	p := e.preview(t, gid, remove(x))
	if !p.Possible || p.AlreadyDiscovered != 0 || p.ChangedPairs != 3 {
		t.Fatalf("aperçu inattendu : %+v", p)
	}
	e.apply(t, gid, remove(x))
	keptRows(t, rowsBefore, e.rows(t, gid))
	got := e.pairs(t, gid)
	if !contains(got, Pair{a, b}) || !contains(got, Pair{f, a}) {
		t.Fatalf("une cible déjà découverte a changé alors qu'une autre réparation existait : %v", got)
	}
	if n := changedCount(before, got); n != 3 {
		t.Fatalf("%d paires nouvelles, attendu 3", n)
	}
}

// F4c.AC16 : en dernier recours, une cible découverte change ; l'aperçu le
// dit, la nouvelle cible est à découvrir et la personne est prévenue.
func TestDiscoveredTargetChangesOnlyAsLastResort(t *testing.T) {
	e := newEnv(t)
	gid := e.group(t, "famille", 2)
	ids := e.people(t, gid, normal, normal, normal, normal)
	a, b, c, x := ids[0], ids[1], ids[2], ids[3]
	e.setPairs(t, gid, ring(a, b, c, x))
	for _, q := range ring(a, b, c, x) {
		e.discover(t, gid, q.Giver, q.Receiver)
	}
	before := e.rows(t, gid)

	p := e.preview(t, gid, remove(x))
	if p.Affected != 1 || p.AlreadyDiscovered != 1 {
		t.Fatalf("aperçu inattendu : %+v", p)
	}
	e.apply(t, gid, remove(x))
	after := e.rows(t, gid)
	keptRows(t, before, after)
	samePairSet(t, e.pairs(t, gid), []Pair{{a, b}, {b, c}, {c, a}})
	if len(e.mail.Sent) != 1 || e.mail.Sent[0].To != e.emails[c] {
		t.Fatalf("la personne dont la cible change doit être prévenue : %+v", e.mail.Sent)
	}
	e.onlyRecipient(t, c, e.mail.Sent[0].Subject, e.mail.Sent[0].Body)
}
