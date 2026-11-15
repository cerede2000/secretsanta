package letterread

import (
	"testing"
)

// AC6 : le cachet est posé par lettre ; lire la première lettre ne pose pas
// le cachet de la seconde et chaque cachet reste sans aucune identité.
func TestTwoTargetsStamp(t *testing.T) {
	e := newEnv(t)
	gid, all := e.twoTargetsGroup(t, "deux-cachets")
	giver, two := e.giverOfTwo(t, gid)
	a, b := two[0], two[1]
	e.openReveal()
	e.discover(t, gid, giver, a.ID)
	e.discover(t, gid, giver, b.ID)

	if e.stamp(t, gid, a.Receiver) || e.stamp(t, gid, b.Receiver) {
		t.Fatal("cachet avant lecture")
	}
	e.mustRead(t, gid, giver, a.ID, a.Receiver)
	if !e.stamp(t, gid, a.Receiver) {
		t.Fatal("cachet absent sur la lettre lue")
	}
	if e.stamp(t, gid, b.Receiver) {
		t.Fatal("lire la première lettre a posé le cachet de la seconde")
	}
	e.mustRead(t, gid, giver, b.ID, b.Receiver)
	for _, p := range two {
		got, err := e.svc.Stamp(ctx, who(gid, p.Receiver), gid)
		// Le cachet est identique pour les deux tirés : un booléen, rien qui
		// dise qui l'a posé ni qu'il offre deux cadeaux.
		if err != nil || got != (Stamp{Read: true}) {
			t.Fatalf("cachet de %d : %+v %v", p.Receiver, got, err)
		}
	}
	// Aucune autre lettre n'a de cachet.
	for _, o := range all {
		if o != a.Receiver && o != b.Receiver && e.stamp(t, gid, o) {
			t.Fatalf("cachet inattendu pour %d", o)
		}
	}
	// L'administrateur voit « lettre lue » pour les deux tirés, sans tireur.
	adm, err := e.svc.AdminRead(ctx, admin(gid), gid)
	if err != nil || len(adm) != 2 || !adm[a.Receiver] || !adm[b.Receiver] || adm[giver] {
		t.Fatalf("vue administrateur : %v %v", adm, err)
	}

	// Modifier la seconde lettre ne retire que son cachet.
	e.write(t, gid, b.Receiver)
	if !e.stamp(t, gid, a.Receiver) || e.stamp(t, gid, b.Receiver) {
		t.Fatalf("cachets après modification : %v %v", e.stamp(t, gid, a.Receiver), e.stamp(t, gid, b.Receiver))
	}
	// Relire la première ne repose pas celui de la seconde.
	e.mustRead(t, gid, giver, a.ID, a.Receiver)
	if e.stamp(t, gid, b.Receiver) {
		t.Fatal("relire la première lettre a reposé le cachet de la seconde")
	}
	e.mustRead(t, gid, giver, b.ID, b.Receiver)
	if !e.stamp(t, gid, b.Receiver) {
		t.Fatal("cachet absent après relecture de la seconde")
	}
}
