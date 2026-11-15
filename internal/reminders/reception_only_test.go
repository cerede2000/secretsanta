package reminders

import (
	"context"
	"testing"
	"time"

	"secretsanta/internal/participants"
)

// F7.AC12 : reception_seule reçoit lien et lettre, jamais de relance de
// révélation, et n'est pas dans la liste des retardataires.
func TestReceptionOnlyGetsLinkAndLetterButNeverReveal(t *testing.T) {
	e := newEnv(t, paris(2026, time.November, 5, 10, 0))
	j := paris(2026, time.November, 1, 10, 0)
	r := e.add(participants.Participant{Name: "Rita", Email: "rita@example.net", Role: participants.RoleReceptionSeule, JoinedAt: j})
	n := e.add(participants.Participant{Name: "Noé", Email: "noe@example.net", JoinedAt: j})
	e.facts.unread[r] = true // même une donnée incohérente ne doit pas la relancer
	e.facts.unread[n] = true
	ctx := context.Background()

	// Lien (auto, jour 1) puis lettre (manuel, jour 2).
	if got := e.tick(t); got != 2 || len(e.to(r)) != 1 {
		t.Fatalf("auto : %d, Rita %d", got, len(e.to(r)))
	}
	e.clk.Set(paris(2026, time.November, 6, 10, 0))
	res, err := e.eng.Remind(ctx, e.gid, KindLetter, []int64{r})
	must(t, err)
	if res.Count(OutcomeQueued) != 1 {
		t.Fatalf("lettre : %+v", res)
	}
	// Révélation : jamais pour Rita.
	res, err = e.eng.Remind(ctx, e.gid, KindRead, []int64{r, n})
	must(t, err)
	if len(res.IDs(OutcomeNotNeeded)) != 1 || res.IDs(OutcomeNotNeeded)[0] != r || len(res.IDs(OutcomeQueued)) != 1 {
		t.Fatalf("révélation : %+v", res)
	}
	lag, err := e.eng.RevealLaggards(ctx, e.gid)
	must(t, err)
	if len(lag) != 1 || lag[0].ID != n {
		t.Fatalf("retardataires : %+v", lag)
	}
	if len(e.to(r)) != 2 {
		t.Fatalf("Rita a %d courriels, 2 attendus (lien, lettre)", len(e.to(r)))
	}
}
