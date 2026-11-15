package givernotify

import (
	"testing"

	"secretsanta/internal/mailunsub"
)

// F7b.AC4 : une seule relance pour un tireur à deux cibles, qui s'arrête quand
// toutes les lettres nouvelles ont été ouvertes.
func TestTwoTargetsSingleReminderUntilAllOpened(t *testing.T) {
	e := newEnv(t, "fr")
	gid, _ := e.twoTargetsGroup(t, "famille")
	giver, ps := e.firstGiverWith(t, gid, 2)
	e.edit(t, gid, ps[0].Receiver, 1)
	e.edit(t, gid, ps[1].Receiver, 1)
	e.clock.Advance(3 * day)
	e.remind(t)
	items := e.q.to(giver, mailunsub.TypeRelances)
	if len(items) != 1 {
		t.Fatalf("%d relances pour deux lettres non ouvertes, 1 attendue", len(items))
	}
	noOtherName(t, e, gid, giver, items[0])
	if e.remind(t) != 0 {
		t.Fatal("pas de seconde relance le même jour")
	}
	// Une lettre ouverte, l'autre non : la relance suivante reste unique.
	e.open(t, gid, ps[0])
	e.clock.Advance(7 * day)
	e.remind(t)
	if n := len(e.q.to(giver, mailunsub.TypeRelances)); n != 2 {
		t.Fatalf("%d relances, 2 attendues (une par échéance)", n)
	}
	// Tout est ouvert : plus de relance.
	e.open(t, gid, ps[1])
	e.clock.Advance(30 * day)
	e.remind(t)
	if n := len(e.q.to(giver, mailunsub.TypeRelances)); n != 2 {
		t.Fatalf("%d relances après ouverture de toutes les lettres", n)
	}
}
