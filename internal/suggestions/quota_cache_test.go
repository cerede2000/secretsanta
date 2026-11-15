package suggestions

import (
	"errors"
	"testing"
	"time"

	"secretsanta/internal/access/limiter"
	"secretsanta/internal/sources"
)

func TestCacheTTLAndQuota(t *testing.T) {
	e := newEnv(t, Options{GiverQuota: 2, TTL: time.Hour, QuotaWin: time.Hour})
	e.search.res = []sources.Suggestion{{Title: "a", URL: "https://x.example/a", Image: "https://x.example/a.png", Price: "9,90 €"}}
	r, err := e.svc.Suggest(ctx, e.req(1))
	if err != nil || r.Items[0].ImageSrc != "/images/abc.png" || r.Items[0].PriceCents != 990 {
		t.Fatalf("%v %v", r, err)
	}
	if r, _ = e.svc.Suggest(ctx, e.req(1)); !r.Cached || e.search.calls() != 1 {
		t.Fatal("cache non servi")
	}
	rq := e.req(1)
	rq.Refresh = true
	if r, _ = e.svc.Suggest(ctx, rq); r.Cached || e.search.calls() != 2 {
		t.Fatal("actualisation sans recherche")
	}
	// Quota atteint : le cache est servi, pas de recherche.
	if r, _ = e.svc.Suggest(ctx, rq); !r.Throttled || e.search.calls() != 2 || len(r.Items) != 1 {
		t.Fatalf("quota : %+v", r)
	}
	// Cible sans cache et quota atteint : refus 429.
	e.targets.by[1] = []int64{2, 3}
	rq.Gift = 2
	if _, err := e.svc.Suggest(ctx, rq); !errors.Is(err, limiter.ErrLimited) {
		t.Fatalf("%v", err)
	}
	// La fenêtre passe : à nouveau permis.
	e.clock.Advance(2 * time.Hour)
	if r, err = e.svc.Suggest(ctx, rq); err != nil || r.Throttled || e.search.calls() != 3 {
		t.Fatalf("%v %v", r, err)
	}
}

func TestUnavailableAndImageFailure(t *testing.T) {
	e := newEnv(t, Options{})
	e.search.err = sources.ErrNoResult
	r, err := e.svc.Suggest(ctx, e.req(1))
	if err != nil || !r.Unavailable || len(r.Items) != 0 {
		t.Fatalf("%v %v", r, err)
	}
	e.search.err = nil
	e.images.fail = true
	e.search.res = []sources.Suggestion{{Title: "a", Image: "https://x.example/a.png"}}
	r, err = e.svc.Suggest(ctx, e.req(1))
	if err != nil || r.Items[0].ImageSrc != "" {
		t.Fatalf("%v %v", r, err)
	}
}
