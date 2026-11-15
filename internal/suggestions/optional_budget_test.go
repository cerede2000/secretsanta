package suggestions

import (
	"testing"

	"secretsanta/internal/sources"
)

func TestFreePriceFilterWithoutBudget(t *testing.T) {
	e := newEnv(t, Options{})
	e.search.res = []sources.Suggestion{sug("petit", "5 €"), sug("moyen", "40 €"), sug("grand", "300 €"), sug("inconnu", "")}
	// F8c.AC4 : sans budget, toutes les gammes.
	r, err := e.svc.Suggest(ctx, e.req(1))
	if err != nil || len(r.Items) != 4 || r.BudgetCents != nil {
		t.Fatalf("%v %v", r, err)
	}
	if e.search.queries[0].Band != sources.BandAll {
		t.Fatalf("tranche : %s", e.search.queries[0].Band)
	}
	req := e.req(1)
	req.MinCents, req.MaxCents = cents(1000), cents(10000)
	r, _ = e.svc.Suggest(ctx, req)
	if got := titles(r.Items); len(got) != 2 || got[0] != "moyen" || got[1] != "inconnu" {
		t.Fatalf("min+max : %v", got)
	}
	req.MinCents, req.MaxCents = cents(10000), nil
	r, _ = e.svc.Suggest(ctx, req)
	if got := titles(r.Items); len(got) != 2 || got[0] != "grand" {
		t.Fatalf("min seul : %v", got)
	}
	req.MinCents, req.MaxCents = nil, cents(1000)
	r, _ = e.svc.Suggest(ctx, req)
	if got := titles(r.Items); got[0] != "petit" {
		t.Fatalf("max seul : %v", got)
	}
	req.MinCents, req.MaxCents = cents(5000), cents(1000)
	if _, err := e.svc.Suggest(ctx, req); err == nil {
		t.Fatal("min > max accepté")
	}
	// Les filtres s'appliquent au cache : une seule recherche réelle.
	if e.search.calls() != 1 {
		t.Fatalf("recherches : %d", e.search.calls())
	}
}

// Budget défini puis retiré, ou retiré puis redéfini : jamais le cache de l'ancien.
func TestBudgetChangesNeverReuseOldCache(t *testing.T) {
	e := newEnv(t, Options{})
	e.search.res = []sources.Suggestion{sug("a", "10 €"), sug("b", "80 €")}

	e.setBudget(cents(2000), 1)
	r, _ := e.svc.Suggest(ctx, e.req(1))
	if len(r.Items) != 1 || e.search.calls() != 1 {
		t.Fatalf("budget 20 : %v", titles(r.Items))
	}
	e.setBudget(nil, 2) // retiré
	r, _ = e.svc.Suggest(ctx, e.req(1))
	if len(r.Items) != 2 || r.Cached || e.search.calls() != 2 || e.search.queries[1].Band != sources.BandAll {
		t.Fatalf("budget retiré : %v cache=%v appels=%d", titles(r.Items), r.Cached, e.search.calls())
	}
	e.setBudget(cents(2000), 3) // redéfini, même montant
	r, _ = e.svc.Suggest(ctx, e.req(1))
	if len(r.Items) != 1 || r.Cached || e.search.calls() != 3 {
		t.Fatalf("budget redéfini : %v cache=%v appels=%d", titles(r.Items), r.Cached, e.search.calls())
	}
	e.setBudget(cents(9000), 4) // modifié
	r, _ = e.svc.Suggest(ctx, e.req(1))
	if len(r.Items) != 2 || r.Cached {
		t.Fatalf("budget modifié : %v", titles(r.Items))
	}
	// Même budget, même version : le cache sert.
	r, _ = e.svc.Suggest(ctx, e.req(1))
	if !r.Cached || e.search.calls() != 4 {
		t.Fatalf("cache non utilisé : %v %d", r.Cached, e.search.calls())
	}
}
