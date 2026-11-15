package suggestions

import (
	"testing"

	"secretsanta/internal/sources"
)

// F8c.AC1 : avec un budget, aucune proposition dont le prix connu le dépasse.
func TestBudgetRespected(t *testing.T) {
	e := newEnv(t, Options{})
	e.setBudget(cents(2500), 1)
	e.search.res = []sources.Suggestion{sug("a", "10,00 €"), sug("b", "25,00 €"), sug("c", "25,01 €"), sug("d", "99 €"), sug("e", "")}
	r, err := e.svc.Suggest(ctx, e.req(1))
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Items) == 0 {
		t.Fatal("aucune proposition")
	}
	for _, it := range r.Items {
		if it.PriceCents > 2500 {
			t.Fatalf("prix %d au-dessus du budget : %s", it.PriceCents, it.Title)
		}
	}
	if got := titles(r.Items); len(got) != 3 || got[0] != "a" || got[1] != "b" || got[2] != "e" {
		t.Fatalf("propositions inattendues : %v", got)
	}
	if string(e.search.queries[0].Band) != "20to50" {
		t.Fatalf("tranche envoyée : %s", e.search.queries[0].Band)
	}
}

// Le budget s'applique aussi à une entrée venue du cache.
func TestBudgetAppliedOnCachedItems(t *testing.T) {
	e := newEnv(t, Options{})
	e.setBudget(cents(1000), 1)
	e.search.res = []sources.Suggestion{sug("a", "5 €"), sug("b", "50 €")}
	for i := 0; i < 2; i++ {
		r, err := e.svc.Suggest(ctx, e.req(1))
		if err != nil || len(r.Items) != 1 || r.Items[0].Title != "a" {
			t.Fatalf("tour %d : %v %v", i, r, err)
		}
	}
}
