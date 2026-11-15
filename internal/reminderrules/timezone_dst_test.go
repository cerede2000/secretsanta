package reminderrules

import (
	"context"
	"testing"
	"time"

	"secretsanta/internal/config"
)

func loadCfg(t *testing.T, env map[string]string) *config.Config {
	t.Helper()
	base := map[string]string{
		config.VarPublicURL:        "https://exemple.test",
		config.VarInstancePassword: "mdp-instance-long",
		config.VarSigningSecret:    "0123456789abcdef0123456789abcdef0123456789abcdef",
	}
	for k, v := range env {
		base[k] = v
	}
	c, err := config.Load(func(k string) string { return base[k] })
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestZoneFromEnvironmentDefaultsToParis(t *testing.T) {
	c := loadCfg(t, nil)
	if c.Timezone != "Europe/Paris" {
		t.Fatalf("défaut : %q", c.Timezone)
	}
	e := newEnv(t, c.Location, time.Date(2026, 11, 16, 9, 0, 0, 0, time.UTC))
	if e.svc.Location().String() != "Europe/Paris" {
		t.Fatalf("fuseau du service : %v", e.svc.Location())
	}
	c2 := loadCfg(t, map[string]string{config.VarTimezone: "America/New_York"})
	s2 := NewService(nil, e.clk, c2.Location)
	// 14 h UTC = 9 h à New York : dans la fenêtre ; à Paris ce serait 15 h aussi, mais 6 h UTC est 1 h à NY.
	if !s2.InWindow(time.Date(2026, 11, 16, 14, 0, 0, 0, time.UTC)) || s2.InWindow(time.Date(2026, 11, 16, 6, 0, 0, 0, time.UTC)) {
		t.Fatal("la fenêtre doit suivre APP_TIMEZONE")
	}
}

func TestSpringForwardKeepsLocalNineOClock(t *testing.T) {
	loc := paris(t)
	e := newEnv(t, loc, time.Date(2027, 3, 20, 9, 0, 0, 0, time.UTC))
	// Fait le 26 mars 2027 à 22 h (CET) ; la première relance (+3 j) tombe le 29 à 22 h (CEST) : hors fenêtre.
	trigger := time.Date(2027, 3, 26, 22, 0, 0, 0, loc)
	due, _ := e.svc.DueAt(DefaultSettings(), trigger, 0)
	if want := time.Date(2027, 3, 30, 9, 0, 0, 0, loc); !due.Equal(want) {
		t.Fatalf("reçu %v, attendu %v", due, want)
	}
	// Fait à 10 h le 26 : +3 j = 10 h locale le 29 (et non 11 h par addition de 72 h).
	due, _ = e.svc.DueAt(DefaultSettings(), time.Date(2027, 3, 26, 10, 0, 0, 0, loc), 0)
	if due.In(loc).Hour() != 10 || due.In(loc).Day() != 29 {
		t.Fatalf("l'heure locale doit être conservée : %v", due.In(loc))
	}
}

func TestFallBackNoShiftNoDuplicate(t *testing.T) {
	loc := paris(t)
	// Le 25 octobre 2026 à 3 h, retour à 2 h : la journée locale dure 25 h.
	e := newEnv(t, loc, time.Date(2026, 10, 24, 22, 0, 0, 0, time.UTC)) // 0 h le 25 à Paris
	ctx := context.Background()
	if ok, _ := e.svc.Claim(ctx, e.gid, 1, "lien"); !ok {
		t.Fatal("première réservation")
	}
	for _, d := range []time.Duration{2 * time.Hour, 3 * time.Hour, 24 * time.Hour} { // 2 h, 3 h (2e), 24 h après : encore le 25
		e.clk.Set(time.Date(2026, 10, 24, 22, 0, 0, 0, time.UTC).Add(d))
		if e.svc.Day(e.clk.Now()) != "2026-10-25" {
			t.Fatalf("jour local inattendu %s", e.svc.Day(e.clk.Now()))
		}
		if ok, _ := e.svc.Claim(ctx, e.gid, 1, "lettre"); ok {
			t.Fatalf("doublon à +%v le jour du changement d'heure", d)
		}
	}
	e.clk.Set(time.Date(2026, 10, 25, 23, 30, 0, 0, time.UTC)) // 0 h 30 le 26
	if ok, _ := e.svc.Claim(ctx, e.gid, 1, "lettre"); !ok {
		t.Fatal("le jour local suivant, une relance est permise")
	}
	// Début de fenêtre le jour du changement : 9 h locales = 8 h UTC (CET).
	if got := e.svc.NextSend(time.Date(2026, 10, 25, 1, 0, 0, 0, time.UTC)); !got.Equal(time.Date(2026, 10, 25, 8, 0, 0, 0, time.UTC)) {
		t.Fatalf("début de fenêtre : %v", got)
	}
}
