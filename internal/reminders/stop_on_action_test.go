package reminders

import (
	"context"
	"testing"
	"time"

	"secretsanta/internal/participants"
)

// F7.AC6 : une relance due n'est pas envoyée si la personne a agi entre-temps.
func TestNoReminderAfterAction(t *testing.T) {
	joined := paris(2026, time.November, 1, 10, 0)
	cases := []struct {
		name string
		act  func(e *env, pid int64)
	}{
		{"lien ouvert et lettre écrite", func(e *env, pid int64) { e.facts.linkOpened[pid] = true; e.facts.written[pid] = true }},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			e := newEnv(t, paris(2026, time.November, 2, 10, 0))
			pid := e.add(participants.Participant{Name: "Léa", Email: "lea@example.net", JoinedAt: joined})
			c.act(e, pid)
			e.clk.Set(paris(2026, time.November, 5, 10, 0)) // la relance était due
			if n := e.tick(t); n != 0 {
				t.Fatalf("%d relance(s) après l'action", n)
			}
		})
	}
}

func TestLinkOpenedStopsOnlyLinkReminder(t *testing.T) {
	e := newEnv(t, paris(2026, time.November, 5, 10, 0))
	pid := e.add(participants.Participant{Name: "Léa", Email: "lea@example.net", JoinedAt: paris(2026, time.November, 1, 10, 0)})
	e.facts.linkOpened[pid] = true
	if n := e.tick(t); n != 1 {
		t.Fatalf("lettre non écrite : %d relance(s), 1 attendue", n)
	}
	if got := e.to(pid)[0].Subject; got == "" {
		t.Fatal("objet vide")
	}
}

func TestLetterWrittenStopsLetterReminder(t *testing.T) {
	e := newEnv(t, paris(2026, time.November, 5, 10, 0))
	pid := e.add(participants.Participant{Name: "Léa", Email: "lea@example.net", JoinedAt: paris(2026, time.November, 1, 10, 0)})
	e.facts.written[pid] = true // le lien n'est pas ouvert : une relance de lien part
	if n := e.tick(t); n != 1 {
		t.Fatalf("%d relance(s), 1 attendue (lien)", n)
	}
	e.clk.Set(paris(2026, time.November, 6, 10, 0))
	e.facts.linkOpened[pid] = true // plus rien à relancer
	if n := e.tick(t); n != 0 {
		t.Fatalf("%d relance(s) après action", n)
	}
}

// Le fait est lu à l'instant de l'envoi, pas à la planification : la relance
// due pendant la nuit n'est pas envoyée si la personne agit avant 9 h.
func TestActionBetweenDueAndWindowStartsCancels(t *testing.T) {
	e := newEnv(t, paris(2026, time.November, 4, 22, 0)) // due (J+3), hors fenêtre
	pid := e.add(participants.Participant{Name: "Léa", Email: "lea@example.net", JoinedAt: paris(2026, time.November, 1, 22, 0)})
	if n := e.tick(t); n != 0 {
		t.Fatalf("hors fenêtre : %d relance(s)", n)
	}
	e.facts.linkOpened[pid] = true
	e.facts.written[pid] = true
	e.clk.Set(paris(2026, time.November, 5, 9, 0))
	if n := e.tick(t); n != 0 {
		t.Fatalf("%d relance(s) malgré l'action", n)
	}
}

func TestRevealReminderStopsOnceDiscovered(t *testing.T) {
	e := newEnv(t, paris(2026, time.November, 5, 10, 0))
	pid := e.add(participants.Participant{Name: "Léa", Email: "lea@example.net", JoinedAt: paris(2026, time.November, 1, 10, 0)})
	e.facts.unread[pid] = true
	res, err := e.eng.Remind(context.Background(), e.gid, KindRead, []int64{pid})
	must(t, err)
	if res.Count(OutcomeQueued) != 1 {
		t.Fatalf("lecture attendue en file : %+v", res)
	}
	e.facts.unread[pid] = false
	e.clk.Set(paris(2026, time.November, 6, 10, 0))
	res, err = e.eng.Remind(context.Background(), e.gid, KindRead, []int64{pid})
	must(t, err)
	if res.Count(OutcomeNotNeeded) != 1 {
		t.Fatalf("ouverture faite : %+v", res)
	}
}
