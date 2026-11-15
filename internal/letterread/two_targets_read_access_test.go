package letterread

import (
	"testing"
)

// AC5 : un tireur à deux cibles lit chaque lettre séparément, seulement
// après la révélation de la cible correspondante, et ne voit jamais la
// lettre d'une cible non révélée.
func TestTwoTargetsReadAccess(t *testing.T) {
	e := newEnv(t)
	gid, all := e.twoTargetsGroup(t, "deux-cibles")
	giver, two := e.giverOfTwo(t, gid)
	a, b := two[0], two[1]

	// Avant la révélation : aucune des deux.
	for _, p := range two {
		_, err := e.svc.Read(ctx, who(gid, giver), gid, p.ID)
		wantRefused(t, err)
	}
	e.openReveal()
	// Révélation ouverte, aucune cible découverte : aucune des deux.
	for _, p := range two {
		_, err := e.svc.Read(ctx, who(gid, giver), gid, p.ID)
		wantRefused(t, err)
	}

	// Première cible découverte : sa lettre seulement.
	e.discover(t, gid, giver, a.ID)
	e.mustRead(t, gid, giver, a.ID, a.Receiver)
	_, err := e.svc.Read(ctx, who(gid, giver), gid, b.ID)
	wantRefused(t, err)
	_, _, err = e.svc.Image(ctx, who(gid, giver), gid, b.ID, 1)
	wantRefused(t, err)

	// Seconde cible découverte : chacune donne sa propre lettre.
	e.discover(t, gid, giver, b.ID)
	la := e.mustRead(t, gid, giver, a.ID, a.Receiver)
	lb := e.mustRead(t, gid, giver, b.ID, b.Receiver)
	if la.ID == lb.ID {
		t.Fatal("les deux cibles donnent la même lettre")
	}

	// Personne d'autre ne lit ces deux lettres, même après avoir découvert
	// ses propres cibles.
	e.discoverAll(t, gid)
	for _, o := range all {
		if o == giver {
			continue
		}
		for _, p := range two {
			_, err := e.svc.Read(ctx, who(gid, o), gid, p.ID)
			wantRefused(t, err)
		}
	}
	// Et le tireur à deux cibles ne lit aucune autre lettre.
	for _, p := range e.pairs(t, gid) {
		if p.Giver == giver {
			continue
		}
		_, err := e.svc.Read(ctx, who(gid, giver), gid, p.ID)
		wantRefused(t, err)
	}

	// Une cible donnée après coup par un ajustement n'est lisible qu'une
	// fois découverte, l'autre restant lisible.
	after, _, newTire := e.swap(t, gid, b.Receiver)
	e.adjust(t, gid, after)
	nb := e.pairOf(t, gid, newTire)
	if nb.Giver != giver {
		t.Fatalf("paire inattendue : %+v", nb)
	}
	_, err = e.svc.Read(ctx, who(gid, giver), gid, nb.ID)
	wantRefused(t, err)
	e.mustRead(t, gid, giver, a.ID, a.Receiver)
	e.discover(t, gid, giver, nb.ID)
	e.mustRead(t, gid, giver, nb.ID, newTire)
}
