package reminderrules

import (
	"testing"
	"time"
)

func TestWindowBounds(t *testing.T) {
	loc := paris(t)
	e := newEnv(t, loc, time.Date(2026, 11, 16, 9, 0, 0, 0, time.UTC))
	at := func(h, m int) time.Time { return time.Date(2026, 11, 16, h, m, 0, 0, loc) }
	cases := []struct {
		t    time.Time
		in   bool
		next time.Time
	}{
		{at(0, 0), false, at(9, 0)},
		{at(8, 59), false, at(9, 0)},
		{at(9, 0), true, at(9, 0)},
		{at(14, 30), true, at(14, 30)},
		{at(19, 59), true, at(19, 59)},
		{at(20, 0), false, time.Date(2026, 11, 17, 9, 0, 0, 0, loc)},
		{at(23, 59), false, time.Date(2026, 11, 17, 9, 0, 0, 0, loc)},
	}
	for _, c := range cases {
		if got := e.svc.InWindow(c.t); got != c.in {
			t.Errorf("%v : InWindow=%v", c.t, got)
		}
		if got := e.svc.NextSend(c.t); !got.Equal(c.next) {
			t.Errorf("%v : NextSend=%v, attendu %v", c.t, got, c.next)
		}
	}
}

func TestWindowUsesConfiguredZoneNotUTC(t *testing.T) {
	e := newEnv(t, paris(t), time.Date(2026, 11, 16, 9, 0, 0, 0, time.UTC))
	// 19 h UTC = 20 h à Paris en hiver : hors fenêtre.
	if e.svc.InWindow(time.Date(2026, 11, 16, 19, 0, 0, 0, time.UTC)) {
		t.Fatal("20 h à Paris est hors fenêtre")
	}
	// 3 h UTC = 4 h à Paris : décalé à 9 h Paris = 8 h UTC.
	got := e.svc.NextSend(time.Date(2026, 11, 16, 3, 0, 0, 0, time.UTC))
	if want := time.Date(2026, 11, 16, 8, 0, 0, 0, time.UTC); !got.Equal(want) {
		t.Fatalf("reçu %v, attendu %v", got, want)
	}
}

func TestDueAtNeverOutsideWindow(t *testing.T) {
	loc := paris(t)
	e := newEnv(t, loc, time.Date(2026, 11, 16, 9, 0, 0, 0, time.UTC))
	st := DefaultSettings()
	for h := 0; h < 24; h++ {
		trigger := time.Date(2026, 11, 1, h, 15, 0, 0, loc)
		for n := 0; n < st.MaxPerType; n++ {
			due, ok := e.svc.DueAt(st, trigger, n)
			if !ok || !e.svc.InWindow(due) {
				t.Fatalf("h=%d n=%d : %v hors fenêtre", h, n, due)
			}
			if due.Before(trigger.AddDate(0, 0, st.FirstDelayDays+n*st.SpacingDays)) {
				t.Fatalf("h=%d n=%d : relance avant son délai", h, n)
			}
		}
		if _, ok := e.svc.DueAt(st, trigger, st.MaxPerType); ok {
			t.Fatal("le plafond doit couper")
		}
	}
	// Fait à 22 h : première relance 3 jours plus tard à 22 h → décalée à 9 h le lendemain.
	due, _ := e.svc.DueAt(st, time.Date(2026, 11, 1, 22, 0, 0, 0, loc), 0)
	if want := time.Date(2026, 11, 5, 9, 0, 0, 0, loc); !due.Equal(want) {
		t.Fatalf("reçu %v, attendu %v", due, want)
	}
}
