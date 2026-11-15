package letterread

import (
	"testing"
)

// AC7 : l'archivage lors d'un ajustement s'applique à la seule lettre de la
// cible changée et laisse l'autre lettre lisible par son tireur.
func TestTwoTargetsArchive(t *testing.T) {
	e := newEnv(t)
	gid, _ := e.twoTargetsGroup(t, "deux-archives")
	giver, two := e.giverOfTwo(t, gid)
	keep, moved := two[0], two[1]
	e.openReveal()
	e.discoverAll(t, gid)
	e.mustRead(t, gid, giver, keep.ID, keep.Receiver)
	e.mustRead(t, gid, giver, moved.ID, moved.Receiver)

	after, otherGiver, otherTire := e.swap(t, gid, moved.Receiver)
	changed := e.adjust(t, gid, after)
	if len(changed) != 2 {
		t.Fatalf("tirés archivés : %v", changed)
	}
	for _, c := range changed {
		if c == keep.Receiver {
			t.Fatal("la lettre de la cible inchangée est archivée")
		}
	}

	// La cible inchangée : pas d'archive, toujours lisible, cachet conservé.
	if e.archives(t, keep.Receiver) != 0 {
		t.Fatal("archive de la lettre inchangée")
	}
	if !e.stamp(t, gid, keep.Receiver) {
		t.Fatal("cachet perdu sur la lettre inchangée")
	}
	if p := e.pairOf(t, gid, keep.Receiver); p.ID != keep.ID || p.Giver != giver {
		t.Fatalf("paire inchangée modifiée : %+v", p)
	}
	e.mustRead(t, gid, giver, keep.ID, keep.Receiver)

	// La cible changée : archivée, plus lisible par le tireur, sans cachet.
	if e.archives(t, moved.Receiver) != 1 {
		t.Fatal("la lettre de la cible changée n'est pas archivée")
	}
	_, err := e.svc.Read(ctx, who(gid, giver), gid, moved.ID)
	wantRefused(t, err)
	if e.stamp(t, gid, moved.Receiver) {
		t.Fatal("cachet de l'ancien tireur resté")
	}
	// Son nouveau tireur la reçoit une fois sa cible découverte ; le tireur
	// à deux cibles n'y a plus accès par sa nouvelle paire non plus.
	np := e.pairOf(t, gid, moved.Receiver)
	if np.Giver != otherGiver {
		t.Fatalf("nouvelle paire : %+v", np)
	}
	_, err = e.svc.Read(ctx, who(gid, giver), gid, np.ID)
	wantRefused(t, err)
	e.discover(t, gid, otherGiver, np.ID)
	e.mustRead(t, gid, otherGiver, np.ID, moved.Receiver)

	// La nouvelle cible du tireur à deux cibles : à découvrir puis lisible.
	nn := e.pairOf(t, gid, otherTire)
	if nn.Giver != giver {
		t.Fatalf("nouvelle cible : %+v", nn)
	}
	_, err = e.svc.Read(ctx, who(gid, giver), gid, nn.ID)
	wantRefused(t, err)
	e.discover(t, gid, giver, nn.ID)
	e.mustRead(t, gid, giver, nn.ID, otherTire)
	e.mustRead(t, gid, giver, keep.ID, keep.Receiver)
}
