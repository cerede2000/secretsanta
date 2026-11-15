package letterread

import (
	"bytes"
	"testing"

	"secretsanta/internal/letter"
	"secretsanta/internal/participants"
)

// AC1 : le tireur lit uniquement la lettre de sa cible, uniquement après la
// révélation, et aucun autre participant n'y accède.
func TestReadAccess(t *testing.T) {
	e := newEnv(t)
	gid := e.group(t, "lecture")
	ids := e.addN(t, gid, "Amandine", 4, participants.RoleNormal)
	e.write(t, gid, ids...)
	img := pngBytes(t)
	// La première personne ajoute une idée avec image.
	imgIdea, err := e.letters.AddIdea(ctx, gid, ids[0], letter.IdeaInput{Title: "Avec image", Image: img})
	if err != nil {
		t.Fatal(err)
	}
	other := e.group(t, "autre-groupe")
	stranger := e.add(t, other, "Etranger", participants.RoleNormal)

	// Avant le tirage : rien n'est lisible.
	for _, g := range ids {
		_, err := e.svc.Read(ctx, who(gid, g), gid, 1)
		wantRefused(t, err)
	}

	e.mustDraw(t, gid)
	ps := e.pairs(t, gid)

	// Tirage fait, révélation fermée : même le tireur ne lit rien, et il ne
	// peut pas découvrir sa cible.
	for _, p := range ps {
		_, err := e.svc.Read(ctx, who(gid, p.Giver), gid, p.ID)
		wantRefused(t, err)
		_, _, err = e.svc.Image(ctx, who(gid, p.Giver), gid, p.ID, imgIdea)
		wantRefused(t, err)
	}

	e.openReveal()

	// Révélation ouverte mais cible pas encore découverte : refus.
	for _, p := range ps {
		_, err := e.svc.Read(ctx, who(gid, p.Giver), gid, p.ID)
		wantRefused(t, err)
	}

	e.discoverAll(t, gid)

	for _, p := range ps {
		// Le tireur lit la lettre de sa cible, et seulement elle.
		l := e.mustRead(t, gid, p.Giver, p.ID, p.Receiver)
		if l.Version < 1 {
			t.Fatalf("version %d", l.Version)
		}
		// Aucun autre participant du groupe n'y accède.
		for _, o := range ids {
			if o == p.Giver {
				continue
			}
			_, err := e.svc.Read(ctx, who(gid, o), gid, p.ID)
			wantRefused(t, err)
		}
		// Ni un participant d'un autre groupe, ni l'administrateur, ni un
		// anonyme, ni le tireur qui se fait passer pour un autre groupe.
		_, err := e.svc.Read(ctx, who(other, stranger), other, p.ID)
		wantRefused(t, err)
		_, err = e.svc.Read(ctx, who(other, p.Giver), gid, p.ID)
		wantRefused(t, err)
		_, err = e.svc.Read(ctx, admin(gid), gid, p.ID)
		wantRefused(t, err)
		_, err = e.svc.Read(ctx, participants.Actor{}, gid, p.ID)
		wantRefused(t, err)
	}

	// Le tireur ne lit aucune autre lettre que celle de sa cible.
	for _, p := range ps {
		for _, q := range ps {
			if q.Giver == p.Giver {
				continue
			}
			_, err := e.svc.Read(ctx, who(gid, p.Giver), gid, q.ID)
			wantRefused(t, err)
		}
	}

	// L'image de l'idée : seulement pour le tireur de sa cible.
	target := e.pairOf(t, gid, ids[0])
	b, mime, err := e.svc.Image(ctx, who(gid, target.Giver), gid, target.ID, imgIdea)
	if err != nil || mime != "image/png" || len(b) == 0 {
		t.Fatalf("image : %q %d %v", mime, len(b), err)
	}
	if !bytes.HasPrefix(b, []byte("\x89PNG")) {
		t.Fatal("l'image servie n'est pas un PNG")
	}
	for _, o := range ids {
		if o == target.Giver {
			continue
		}
		_, _, err := e.svc.Image(ctx, who(gid, o), gid, target.ID, imgIdea)
		wantRefused(t, err)
		// Même avec sa propre cible, une idée d'une autre lettre est refusée.
		mine := e.pairs(t, gid)
		for _, q := range mine {
			if q.Giver == o && q.Receiver != ids[0] {
				_, _, err := e.svc.Image(ctx, who(gid, o), gid, q.ID, imgIdea)
				wantRefused(t, err)
			}
		}
	}
}
