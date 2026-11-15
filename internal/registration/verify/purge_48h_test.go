package verify

import (
	"testing"
	"time"

	"secretsanta/internal/registration/invite"
)

// F11.AC16 : une inscription en attente expire après 48 heures (horloge
// simulée), est alors purgée par la tâche planifiée, et son code ne
// fonctionne plus, même renvoyé juste avant l'échéance.
func TestPendingExpiresAndIsPurgedAfter48Hours(t *testing.T) {
	e := newEnv(t)
	ctx := t.Context()
	r := e.register(t, e.g1, "Léa", "Martin", "lea@example.org")
	other := e.register(t, e.g2, "Noé", "Bernard", "noe@example.org")
	if e.sched.Pending() != 2 {
		t.Fatalf("%d purges planifiées, 2 attendues", e.sched.Pending())
	}

	// Juste avant l'échéance : toujours en attente ; un nouveau code est
	// émis, valable 15 minutes, donc au-delà des 48 heures.
	e.clock.Advance(48*time.Hour - 5*time.Minute)
	if n := e.pendingCount(t, e.g1); n != 1 {
		t.Fatalf("PendingCount avant l'échéance = %d", n)
	}
	if _, err := e.svc.ResendCode(ctx, admin(e.g1), e.g1, r.id); err != nil {
		t.Fatal(err)
	}
	late := e.code(t, r)
	if n, err := e.sched.Tick(ctx); err != nil || n != 0 {
		t.Fatalf("purge avant l'échéance : %d %v", n, err)
	}

	// Après 48 heures : expirée (plus comptée, code refusé) avant même la
	// purge.
	e.clock.Advance(5*time.Minute + time.Second)
	if n := e.pendingCount(t, e.g1); n != 0 {
		t.Fatalf("PendingCount après expiration = %d", n)
	}
	_, err := e.svc.VerifyCode(ctx, e.g1, r.ref, late)
	mustRefused(t, err)

	// La tâche planifiée purge les deux groupes, chacun à son échéance.
	if n, err := e.sched.Tick(ctx); err != nil || n != 2 {
		t.Fatalf("purge : %d tâches, %v", n, err)
	}
	for _, g := range []int64{e.g1, e.g2} {
		all, err := e.pending.ListPending(ctx, g, time.Time{})
		if err != nil {
			t.Fatal(err)
		}
		if len(all) != 0 {
			t.Fatalf("groupe %d : %d inscriptions non purgées", g, len(all))
		}
	}
	_, err = e.svc.VerifyCode(ctx, e.g1, r.ref, late)
	mustRefused(t, err)
	_, err = e.svc.VerifyCode(ctx, e.g2, other.ref, e.code(t, other))
	mustRefused(t, err)
	if len(e.participants(t, e.g1)) != 0 || len(e.welcome.all()) != 0 {
		t.Fatal("une inscription expirée est devenue participante")
	}
}

// Sans tâche planifiée (redémarrage), PurgeExpired rattrape les inscriptions
// expirées et seulement elles.
func TestPurgeExpiredSweepsOnlyExpired(t *testing.T) {
	e := newEnv(t)
	ctx := t.Context()
	old := e.register(t, e.g1, "Léa", "Martin", "lea@example.org")
	e.clock.Advance(24 * time.Hour)
	fresh := e.register(t, e.g1, "Noé", "Bernard", "noe@example.org")
	e.clock.Advance(24*time.Hour + time.Minute)

	n, err := e.svc.PurgeExpired(ctx, e.g1)
	if err != nil || n != 1 {
		t.Fatalf("PurgeExpired = %d, %v", n, err)
	}
	all, err := e.pending.ListPending(ctx, e.g1, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 1 || all[0].ID != fresh.id {
		t.Fatalf("restantes : %+v", all)
	}
	_, err = e.svc.VerifyCode(ctx, e.g1, old.ref, e.code(t, old))
	mustRefused(t, err)
	if e.pendingCount(t, e.g1) != 1 {
		t.Fatal("l'inscription récente a disparu")
	}
	if invite.PendingTTL != 48*time.Hour {
		t.Fatalf("durée de vie %v", invite.PendingTTL)
	}
}
