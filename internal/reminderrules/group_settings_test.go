package reminderrules

import (
	"context"
	"testing"
	"time"

	"secretsanta/internal/shared/errs"
)

func TestDefaultsWhenNoRow(t *testing.T) {
	e := newEnv(t, time.UTC, time.Date(2026, 11, 15, 10, 0, 0, 0, time.UTC))
	st, err := e.svc.Settings(context.Background(), e.gid)
	if err != nil || st != DefaultSettings() {
		t.Fatalf("défauts attendus, reçu %+v (%v)", st, err)
	}
	for n, want := range []int{3, 7, 11} {
		off, ok := st.Offset(n)
		if !ok || off != time.Duration(want)*24*time.Hour {
			t.Fatalf("relance %d : %v %v, attendu %d jours", n, off, ok, want)
		}
	}
	if _, ok := st.Offset(3); ok {
		t.Fatal("le plafond de 3 doit couper la quatrième relance")
	}
}

func TestSettingsRoundTripAndPerGroup(t *testing.T) {
	e := newEnv(t, time.UTC, time.Date(2026, 11, 15, 10, 0, 0, 0, time.UTC))
	ctx := context.Background()
	want := Settings{Enabled: false, FirstDelayDays: 2, SpacingDays: 5, MaxPerType: 4}
	if err := e.svc.SetSettings(ctx, e.gid, want); err != nil {
		t.Fatal(err)
	}
	got, err := e.svc.Settings(ctx, e.gid)
	if err != nil || got != want {
		t.Fatalf("reçu %+v (%v), attendu %+v", got, err, want)
	}
	want.Enabled = true
	if err := e.svc.SetSettings(ctx, e.gid, want); err != nil {
		t.Fatal(err)
	}
	if got, _ := e.svc.Settings(ctx, e.gid); got != want {
		t.Fatalf("mise à jour perdue : %+v", got)
	}
	if err := e.svc.SetSettings(ctx, 9999, want); !errs.Is(err, errs.KindNotFound) {
		t.Fatalf("groupe inconnu : %v", err)
	}
}

func TestOutOfBoundsRefused(t *testing.T) {
	e := newEnv(t, time.UTC, time.Date(2026, 11, 15, 10, 0, 0, 0, time.UTC))
	ctx := context.Background()
	ok := DefaultSettings()
	if err := e.svc.SetSettings(ctx, e.gid, ok); err != nil {
		t.Fatal(err)
	}
	bad := map[string]Settings{
		"délai 0":       {Enabled: true, FirstDelayDays: 0, SpacingDays: 4, MaxPerType: 3},
		"délai 31":      {Enabled: true, FirstDelayDays: 31, SpacingDays: 4, MaxPerType: 3},
		"espacement 0":  {Enabled: true, FirstDelayDays: 3, SpacingDays: 0, MaxPerType: 3},
		"espacement 31": {Enabled: true, FirstDelayDays: 3, SpacingDays: 31, MaxPerType: 3},
		"plafond 0":     {Enabled: true, FirstDelayDays: 3, SpacingDays: 4, MaxPerType: 0},
		"plafond 6":     {Enabled: true, FirstDelayDays: 3, SpacingDays: 4, MaxPerType: 6},
		"négatif":       {Enabled: true, FirstDelayDays: -1, SpacingDays: 4, MaxPerType: 3},
	}
	for name, st := range bad {
		if err := e.svc.SetSettings(ctx, e.gid, st); !errs.Is(err, errs.KindValidation) {
			t.Errorf("%s : refus attendu, reçu %v", name, err)
		}
	}
	if got, _ := e.svc.Settings(ctx, e.gid); got != ok {
		t.Fatalf("un refus ne doit rien modifier : %+v", got)
	}
}
