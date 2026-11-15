package reminderrules

import (
	"context"
	"testing"
	"time"
)

func TestOnePerPersonPerDayAllKinds(t *testing.T) {
	e := newEnv(t, paris(t), time.Date(2026, 11, 16, 9, 30, 0, 0, time.UTC))
	ctx := context.Background()
	first, err := e.svc.Claim(ctx, e.gid, 1, "lien")
	if err != nil || !first {
		t.Fatalf("première réservation : %v %v", first, err)
	}
	for _, kind := range []string{"lettre", "lien", "tireur"} {
		ok, err := e.svc.Claim(ctx, e.gid, 1, kind)
		if err != nil || ok {
			t.Fatalf("%s : une seconde relance le même jour doit être refusée (%v, %v)", kind, ok, err)
		}
	}
	if ok, _ := e.svc.Claim(ctx, e.gid, 2, "lettre"); !ok {
		t.Fatal("une autre personne n'est pas concernée")
	}
	if sent, _ := e.svc.SentToday(ctx, e.gid, 1); !sent {
		t.Fatal("SentToday doit être vrai")
	}
	e.clk.Advance(24 * time.Hour)
	if ok, _ := e.svc.Claim(ctx, e.gid, 1, "lettre"); !ok {
		t.Fatal("le lendemain, une relance est de nouveau permise")
	}
}

func TestDayBoundaryIsLocalMidnight(t *testing.T) {
	// 22 h 30 à Paris le 15 novembre ; 23 h 30 UTC = 0 h 30 le 16.
	e := newEnv(t, paris(t), time.Date(2026, 11, 15, 21, 30, 0, 0, time.UTC))
	ctx := context.Background()
	if ok, _ := e.svc.Claim(ctx, e.gid, 1, "lien"); !ok {
		t.Fatal("première réservation")
	}
	e.clk.Set(time.Date(2026, 11, 15, 22, 30, 0, 0, time.UTC)) // 23 h 30 locale, même jour
	if ok, _ := e.svc.Claim(ctx, e.gid, 1, "lettre"); ok {
		t.Fatal("même jour local : refus attendu")
	}
	e.clk.Set(time.Date(2026, 11, 15, 23, 30, 0, 0, time.UTC)) // 0 h 30 le 16
	if ok, _ := e.svc.Claim(ctx, e.gid, 1, "lettre"); !ok {
		t.Fatal("jour local suivant : réservation attendue")
	}
}

func TestReleaseFreesTheDay(t *testing.T) {
	e := newEnv(t, paris(t), time.Date(2026, 11, 16, 9, 30, 0, 0, time.UTC))
	ctx := context.Background()
	_, _ = e.svc.Claim(ctx, e.gid, 1, "lien")
	if err := e.svc.Release(ctx, e.gid, 1); err != nil {
		t.Fatal(err)
	}
	if ok, _ := e.svc.Claim(ctx, e.gid, 1, "lettre"); !ok {
		t.Fatal("après Release, la réservation est possible")
	}
}
