package reminders

import (
	"testing"
	"time"

	"secretsanta/internal/participants"
)

// F7.AC5 : 3 puis 7 jours par défaut, plafond de 3 par type et par personne.
func TestAutomaticDefaultsThreeThenSevenDaysCapThree(t *testing.T) {
	joined := paris(2026, time.November, 1, 10, 0)
	e := newEnv(t, joined)
	pid := e.add(participants.Participant{Name: "Léa", Email: "lea@example.net", JoinedAt: joined})
	// Le lien est ouvert : seule la relance « lettre » reste à envoyer.
	e.facts.linkOpened[pid] = true

	e.clk.Set(paris(2026, time.November, 3, 10, 0)) // J+2 : trop tôt
	if n := e.tick(t); n != 0 {
		t.Fatalf("J+2 : %d relance(s), 0 attendue", n)
	}
	e.clk.Set(paris(2026, time.November, 4, 10, 0)) // J+3
	if n := e.tick(t); n != 1 {
		t.Fatalf("J+3 : %d relance(s), 1 attendue", n)
	}
	e.clk.Set(paris(2026, time.November, 7, 10, 0)) // J+6 : pas encore
	if n := e.tick(t); n != 0 {
		t.Fatalf("J+6 : %d relance(s), 0 attendue", n)
	}
	e.clk.Set(paris(2026, time.November, 8, 10, 0)) // J+7
	if n := e.tick(t); n != 1 {
		t.Fatalf("J+7 : %d relance(s), 1 attendue", n)
	}
	// Troisième relance par défaut : J+11 (3 + 2×4) ; au-delà, plus rien.
	e.clk.Set(paris(2026, time.November, 12, 10, 0))
	if n := e.tick(t); n != 1 {
		t.Fatalf("J+11 : %d relance(s), 1 attendue", n)
	}
	e.clk.Set(paris(2026, time.December, 20, 10, 0))
	if n := e.tick(t); n != 0 {
		t.Fatalf("plafond dépassé : %d relance(s)", n)
	}
	if got := len(e.to(pid)); got != 3 {
		t.Fatalf("%d courriels, 3 attendus (plafond)", got)
	}
}

func TestAutomaticDisabledForGroupSendsNothing(t *testing.T) {
	joined := paris(2026, time.November, 1, 10, 0)
	e := newEnv(t, paris(2026, time.November, 10, 10, 0))
	e.add(participants.Participant{Name: "Léa", Email: "lea@example.net", JoinedAt: joined})
	st, _ := e.rules.Settings(t.Context(), e.gid)
	st.Enabled = false
	must(t, e.rules.SetSettings(t.Context(), e.gid, st))
	if n := e.tick(t); n != 0 {
		t.Fatalf("groupe désactivé : %d relance(s)", n)
	}
}
