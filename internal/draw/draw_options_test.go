package draw

import (
	"context"
	"testing"
	"time"

	"secretsanta/internal/participants"
)

// fakeOptions est une source d'options de tirage fixe.
type fakeOptions struct{ excludePrevious, antiSwap bool }

func (f *fakeOptions) DrawOptions(context.Context, int64) (bool, bool, error) {
	return f.excludePrevious, f.antiSwap, nil
}

type pairKey struct{ giver, receiver int64 }

func (e *env) pairSet(t *testing.T, drawID int64) map[pairKey]bool {
	t.Helper()
	out := map[pairKey]bool{}
	for _, p := range e.rawPairs(t, drawID) {
		out[pairKey{p.Giver, p.Receiver}] = true
	}
	return out
}

// Option « exclure l'année précédente » : aucun tirage de l'année suivante ne
// reprend une paire du dernier tirage de l'année précédente ; désactivée, la
// même paire reste possible.
func TestPreviousYearOptionReachesTheEngine(t *testing.T) {
	e := newEnv(t)
	opts := &fakeOptions{excludePrevious: true}
	e.svc.settings = opts
	gid := e.group(t, "an-dernier", 0, nil)
	e.addN(t, gid, "Xavarin", 4, participants.RoleNormal)

	last := e.mustDraw(t, gid)
	old := e.pairSet(t, last.DrawID)
	e.clock.Advance(366 * 24 * time.Hour) // 2027
	for i := 0; i < 25; i++ {
		o := e.mustRedraw(t, gid)
		for p := range e.pairSet(t, o.DrawID) {
			if old[p] {
				t.Fatalf("paire de l'année précédente reprise au tirage %d : %v", i, p)
			}
		}
	}
	// Désactivée : au moins une paire de 2026 revient (3 affectations de 9 sur 4
	// personnes ne la contiennent pas, soit 60 tirages sans retour très improbables).
	opts.excludePrevious = false
	seen := false
	for i := 0; i < 60 && !seen; i++ {
		for p := range e.pairSet(t, e.mustRedraw(t, gid).DrawID) {
			seen = seen || old[p]
		}
	}
	if !seen {
		t.Fatal("option désactivée : aucune paire de l'année précédente en 60 tirages")
	}
}

// Sans tirage l'année précédente, l'option n'exclut rien (pas d'erreur).
func TestPreviousYearOptionWithoutHistory(t *testing.T) {
	e := newEnv(t)
	e.svc.settings = &fakeOptions{excludePrevious: true}
	gid := e.group(t, "sans-histoire", 0, nil)
	e.addN(t, gid, "Xavarin", 3, participants.RoleNormal)
	e.mustDraw(t, gid)
}

// Option anti-échange : jamais A -> B avec B -> A ; désactivée, l'échange existe.
func TestAntiSwapOptionReachesTheEngine(t *testing.T) {
	e := newEnv(t)
	opts := &fakeOptions{antiSwap: true}
	e.svc.settings = opts
	gid := e.group(t, "anti-echange", 0, nil)
	e.addN(t, gid, "Xavarin", 4, participants.RoleNormal)
	e.mustDraw(t, gid)
	swapped := func(set map[pairKey]bool) bool {
		for p := range set {
			if set[pairKey{p.receiver, p.giver}] {
				return true
			}
		}
		return false
	}
	for i := 0; i < 30; i++ {
		if swapped(e.pairSet(t, e.mustRedraw(t, gid).DrawID)) {
			t.Fatalf("échange A <-> B au tirage %d avec l'option active", i)
		}
	}
	opts.antiSwap = false
	seen := false
	for i := 0; i < 60 && !seen; i++ {
		seen = swapped(e.pairSet(t, e.mustRedraw(t, gid).DrawID))
	}
	if !seen {
		t.Fatal("option désactivée : aucun échange en 60 tirages")
	}
}
