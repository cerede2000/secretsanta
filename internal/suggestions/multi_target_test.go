package suggestions

import (
	"testing"

	"secretsanta/internal/letter"
	"secretsanta/internal/participants"
	"secretsanta/internal/shared/errs"
	"secretsanta/internal/sources"
)

// F8c.AC8 : suggestions et cache distincts par cible, accès réservé au tireur,
// reception_seule refusée.
func TestTwoTargets(t *testing.T) {
	e := newEnv(t, Options{})
	e.targets.by[1] = []int64{2, 3}
	e.letters.by[2] = &letter.Letter{Version: 1, Ideas: []letter.Idea{{Title: "aquarelle"}}}
	e.letters.by[3] = &letter.Letter{Version: 1, Ideas: []letter.Idea{{Title: "trottinette"}}}
	e.search.res = []sources.Suggestion{sug("a", "")}

	g1 := e.req(1)
	g1.Gift = 1
	g2 := e.req(1)
	g2.Gift = 2
	if _, err := e.svc.Suggest(ctx, g1); err != nil {
		t.Fatal(err)
	}
	if _, err := e.svc.Suggest(ctx, g2); err != nil {
		t.Fatal(err)
	}
	if e.search.calls() != 2 || e.search.queries[0].Keywords[0] != "aquarelle" || e.search.queries[1].Keywords[0] != "trottinette" {
		t.Fatalf("mots-clés par cible : %+v", e.search.queries)
	}
	r, _ := e.svc.Suggest(ctx, g1)
	r2, _ := e.svc.Suggest(ctx, g2)
	if !r.Cached || !r2.Cached || e.search.calls() != 2 {
		t.Fatal("cache non propre à chaque cible")
	}
	var n int
	_ = e.db.QueryRow(`SELECT count(DISTINCT receiver_id) FROM suggestion_cache WHERE giver_id = 1`).Scan(&n)
	if n != 2 {
		t.Fatalf("entrées de cache par cible : %d", n)
	}
	g3 := e.req(1)
	g3.Gift = 3
	_, err := e.svc.Suggest(ctx, g3)
	wantKind(t, err, errs.KindNotFound)

	// Le tiré, un autre tireur à une cible, une personne reception_seule.
	for name, giver := range map[string]int64{"tiré": 2, "sans cible": 3, "reception_seule": 4} {
		_, err := e.svc.Suggest(ctx, e.req(giver))
		if err == nil || errs.KindOf(err) != errs.KindForbidden {
			t.Fatalf("%s : %v", name, err)
		}
	}
	// Un autre tireur n'obtient jamais le cache d'autrui.
	e.targets.by[3] = []int64{2}
	before := e.search.calls()
	if r, err := e.svc.Suggest(ctx, e.req(3)); err != nil || r.Cached || e.search.calls() != before+1 {
		t.Fatalf("cache partagé entre tireurs : %v %v", r, err)
	}
	_ = participants.RoleReceptionSeule
}
