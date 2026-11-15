package reminders

import (
	"context"
	"strings"
	"testing"
	"time"

	"secretsanta/internal/participants"
)

// F7.AC9 : aucun courriel de relance ne contient le tireur ni une autre personne.
func TestReminderMailsNameNoOneButTheRecipient(t *testing.T) {
	e := newEnv(t, paris(2026, time.November, 5, 10, 0))
	j := paris(2026, time.November, 1, 10, 0)
	lea := e.add(participants.Participant{Name: "Léa Durand", FirstName: "Léa", Email: "lea@example.net", JoinedAt: j})
	others := []string{"Hugo", "Camille", "Nathan", "Durand-Pierre"}
	for _, o := range others {
		e.add(participants.Participant{Name: o, FirstName: o, Email: strings.ToLower(o) + "@example.net", JoinedAt: j})
	}
	e.facts.unread[lea] = true
	ctx := context.Background()
	for _, k := range []Kind{KindLink, KindLetter, KindRead} {
		e.clk.Advance(24 * time.Hour)
		if _, err := e.eng.Remind(ctx, e.gid, k, []int64{lea}); err != nil {
			t.Fatal(err)
		}
	}
	if len(e.to(lea)) != 3 {
		t.Fatalf("%d courriels pour Léa", len(e.to(lea)))
	}
	for _, it := range e.to(lea) {
		all := it.Subject + "\n" + it.Text + "\n" + it.HTML
		for _, o := range others {
			if strings.Contains(all, o) {
				t.Errorf("%q cité dans %q", o, it.Subject)
			}
		}
		for _, w := range []string{"tireur", "giver", "cible", "target", "Père Noël de"} {
			if strings.Contains(strings.ToLower(all), strings.ToLower(w)) {
				t.Errorf("mot interdit %q dans %q", w, it.Subject)
			}
		}
		if !strings.Contains(all, "Léa") {
			t.Errorf("le destinataire n'est pas nommé dans %q", it.Subject)
		}
	}
}
