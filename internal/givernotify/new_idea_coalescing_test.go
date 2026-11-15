package givernotify

import (
	"testing"
	"time"

	"secretsanta/internal/mailunsub"
)

// F7b.AC2 : plusieurs modifications en moins de 10 minutes, un seul courriel,
// 10 minutes après la dernière.
func TestNewIdeaSingleMailTenMinutesAfterLastEdit(t *testing.T) {
	e := newEnv(t, "fr")
	gid, _ := e.singleGroup(t, "famille")
	giver, ps := e.firstGiverWith(t, gid, 1)
	r := ps[0].Receiver
	e.edit(t, gid, r, 1)
	e.clock.Advance(4 * time.Minute)
	e.edit(t, gid, r, 2)
	e.clock.Advance(4 * time.Minute)
	e.edit(t, gid, r, 3)
	if n := len(e.q.to(giver, mailunsub.TypeNouvelleIdee)); n != 3 {
		t.Fatalf("%d remises à la file (même clé attendue, 3 mises à jour)", n)
	}
	e.clock.Advance(9 * time.Minute)
	if e.sendMail(t) != 0 {
		t.Fatal("aucun envoi avant 10 minutes après la dernière modification")
	}
	e.clock.Advance(time.Minute)
	if e.sendMail(t) != 1 || e.tr.count() != 1 {
		t.Fatalf("un seul courriel attendu, %d envoyés", e.tr.count())
	}
	e.clock.Advance(time.Hour)
	if e.sendMail(t) != 0 || e.tr.count() != 1 {
		t.Fatal("pas de second courriel")
	}
}

// F7b.AC2 : jamais plus de 60 minutes après la première modification.
func TestNewIdeaNeverLaterThanSixtyMinutes(t *testing.T) {
	e := newEnv(t, "fr")
	gid, _ := e.singleGroup(t, "famille")
	_, ps := e.firstGiverWith(t, gid, 1)
	r := ps[0].Receiver
	start := e.clock.Now()
	e.edit(t, gid, r, 0)
	for i := 1; i <= 11; i++ { // une modification toutes les 5 minutes
		e.clock.Advance(5 * time.Minute)
		e.edit(t, gid, r, i)
		if e.sendMail(t) != 0 {
			t.Fatalf("envoi prématuré à +%d min", i*5)
		}
	}
	e.clock.Set(start.Add(59 * time.Minute))
	if e.sendMail(t) != 0 {
		t.Fatal("pas d'envoi à +59 min")
	}
	e.clock.Set(start.Add(60 * time.Minute))
	if e.sendMail(t) != 1 || e.tr.count() != 1 {
		t.Fatalf("envoi attendu à +60 min, %d envoyés", e.tr.count())
	}
}

// Pas de courriel avant que le tireur puisse lire la lettre.
func TestNewIdeaWaitsForRevealGate(t *testing.T) {
	e := newEnv(t, "fr")
	gid := e.group(t, "famille")
	all := e.addN(t, gid, "Mederic", 6, "normal")
	e.write(t, gid, all...)
	e.mustDraw(t, gid)
	// horloge avant la date de révélation
	e.edit(t, gid, all[0], 1)
	e.clock.Advance(time.Hour)
	if len(e.q.items) != 0 || e.sendMail(t) != 0 {
		t.Fatal("aucun courriel avant la révélation")
	}
}
