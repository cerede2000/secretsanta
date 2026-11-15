package givernotify

import (
	"testing"
	"time"

	"secretsanta/internal/mailunsub"
)

// F7b.AC3 : un tireur à deux cibles dont les deux lettres changent en moins de
// 10 minutes reçoit un seul courriel, sans nom de cible ni de tireur.
func TestTwoTargetsOneMailForBothLetters(t *testing.T) {
	e := newEnv(t, "fr")
	gid, _ := e.twoTargetsGroup(t, "famille")
	giver, ps := e.firstGiverWith(t, gid, 2)
	e.edit(t, gid, ps[0].Receiver, 1)
	e.clock.Advance(5 * time.Minute)
	e.edit(t, gid, ps[1].Receiver, 1)
	e.clock.Advance(9 * time.Minute)
	if e.sendMail(t) != 0 {
		t.Fatal("rien avant 10 minutes après la dernière modification")
	}
	e.clock.Advance(time.Minute)
	if e.sendMail(t) != 1 || e.tr.count() != 1 {
		t.Fatalf("un seul courriel attendu, %d envoyés", e.tr.count())
	}
	items := e.q.to(giver, mailunsub.TypeNouvelleIdee)
	if len(items) != 2 {
		t.Fatalf("%d remises à la file", len(items))
	}
	for _, it := range items {
		if it.CoalesceKey != CoalesceKey(gid, giver) {
			t.Fatalf("clé de regroupement %q : celle du tireur attendue", it.CoalesceKey)
		}
		noOtherName(t, e, gid, giver, it)
	}
}

// Les deux lettres, en même temps ou non, ne sont jamais deux courriels : la
// clé est celle du tireur.
func TestTwoTargetsKeyIsTheGiversNotTheLetters(t *testing.T) {
	e := newEnv(t, "fr")
	gid, _ := e.twoTargetsGroup(t, "famille")
	giver, ps := e.firstGiverWith(t, gid, 2)
	e.edit(t, gid, ps[0].Receiver, 1)
	e.edit(t, gid, ps[1].Receiver, 1)
	e.clock.Advance(10 * time.Minute)
	e.sendMail(t)
	if got := e.tr.count(); got != 1 {
		t.Fatalf("%d courriels pour %d", got, giver)
	}
}
