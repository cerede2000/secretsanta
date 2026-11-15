package reminders

import (
	"testing"
	"time"

	"secretsanta/internal/participants"
)

// F7.AC11 : le délai de la relance « lettre » part de joined_at (vérification
// de l'inscription par lien), pas de la création du groupe.
func TestLetterReminderCountsFromJoinedAt(t *testing.T) {
	// Le groupe est créé tôt ; Léa s'inscrit par lien et vérifie le 20 novembre.
	e := newEnv(t, paris(2026, time.November, 1, 10, 0))
	verified := paris(2026, time.November, 20, 14, 0)
	pid := e.add(participants.Participant{Name: "Léa", Email: "lea@example.net", JoinedAt: verified})
	e.facts.linkOpened[pid] = true // seule la lettre reste

	e.clk.Set(paris(2026, time.November, 4, 10, 0)) // 3 jours après la création du groupe
	if n := e.tick(t); n != 0 {
		t.Fatalf("relance comptée depuis la création du groupe : %d", n)
	}
	e.clk.Set(paris(2026, time.November, 22, 14, 0)) // 2 jours après la vérification
	if n := e.tick(t); n != 0 {
		t.Fatalf("J+2 après vérification : %d", n)
	}
	e.clk.Set(paris(2026, time.November, 23, 14, 0)) // J+3
	if n := e.tick(t); n != 1 {
		t.Fatalf("J+3 après vérification : %d relance(s), 1 attendue", n)
	}
}

func TestReminderDueOutsideWindowIsShiftedToNextMorning(t *testing.T) {
	e := newEnv(t, paris(2026, time.November, 4, 22, 30))
	pid := e.add(participants.Participant{Name: "Léa", Email: "lea@example.net", JoinedAt: paris(2026, time.November, 1, 22, 30)})
	if n := e.tick(t); n != 0 {
		t.Fatalf("nuit : %d", n)
	}
	e.clk.Set(paris(2026, time.November, 5, 9, 0))
	if n := e.tick(t); n != 1 || len(e.to(pid)) != 1 {
		t.Fatalf("9 h : %d", n)
	}
}
