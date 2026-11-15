package reminders

import (
	"context"
	"testing"
	"time"

	"secretsanta/internal/participants"
)

// F7.AC13 : une adresse supprimée ne reçoit rien et la manuelle la signale.
func TestSuppressedAddressExcluded(t *testing.T) {
	e := newEnv(t, paris(2026, time.November, 5, 10, 0))
	j := paris(2026, time.November, 1, 10, 0)
	a := e.add(participants.Participant{Name: "Anna", Email: "Anna@Example.net", JoinedAt: j})
	b := e.add(participants.Participant{Name: "Bruno", Email: "bruno@example.net", JoinedAt: j})
	ctx := context.Background()
	must(t, e.sup.Add(ctx, e.gid, "anna@example.net", "5.1.1"))

	if n := e.tick(t); n != 1 || len(e.to(a)) != 0 {
		t.Fatalf("auto : %d relance(s), Anna %d", n, len(e.to(a)))
	}
	res, err := e.eng.Remind(ctx, e.gid, KindLetter, []int64{a, b})
	must(t, err)
	if got := res.IDs(OutcomeAddressRefused); len(got) != 1 || got[0] != a {
		t.Fatalf("adresse refusée : %+v", res)
	}
	if len(e.to(a)) != 0 {
		t.Fatal("courriel mis en file pour une adresse supprimée")
	}
}
