package targetpage

import (
	"testing"
	"time"
)

const bannerMark = `class="draw-banner"`

// Sans budget, aucune mention ; défini, il apparaît avec le bandeau ; vu, le
// bandeau disparaît mais le budget reste ; retiré, la mention disparaît ;
// redéfini, le bandeau revient.
func checkBudgetLifecycle(t *testing.T, e *env, gid int64, slug string, page func() string) {
	t.Helper()
	e.setBudget(t, gid, nil, 0)
	body := page()
	mustNotContain(t, body, "Budget")
	mustNotContain(t, body, "25 €")
	mustNotContain(t, body, bannerMark)

	e.setBudget(t, gid, ptr(2500), 1)
	body = page()
	mustContain(t, body, "25 €")
	mustContain(t, body, "Budget mis à jour : 25 €")
	mustContain(t, body, bannerMark)

	// Un simple affichage ne marque rien comme vu (un GET ne modifie rien).
	mustContain(t, page(), bannerMark)

	w := e.post(t, slug, "/budget-vu", "", map[string][]string{"version": {"1"}})
	if w.Code != 303 {
		t.Fatalf("budget-vu : %d", w.Code)
	}
	body = page()
	mustNotContain(t, body, bannerMark)
	mustContain(t, body, "25 €")

	e.setBudget(t, gid, nil, 2)
	body = page()
	mustNotContain(t, body, "Budget")
	mustNotContain(t, body, "25 €")
	mustNotContain(t, body, bannerMark)

	e.setBudget(t, gid, ptr(3050), 3)
	body = page()
	mustContain(t, body, "30,50 €")
	mustContain(t, body, bannerMark)
}

func TestBudgetDisplayOnWaitPage(t *testing.T) {
	e := newEnv(t)
	s := newScene(t, e, "attente-budget", at(48*time.Hour), true)
	e.login(t, s.gid, s.normal[0])
	checkBudgetLifecycle(t, e, s.gid, s.slug, func() string {
		w := e.do("GET", "/"+s.slug+"/participant", "", nil)
		if w.Code != 200 {
			t.Fatalf("attente : %d", w.Code)
		}
		mustContain(t, w.Body.String(), `data-countdown`)
		return w.Body.String()
	})
}

func TestBudgetDisplayOnTargetCard(t *testing.T) {
	e := newEnv(t)
	s := newScene(t, e, "carte-budget", nil, true)
	e.discover(t, s.gid, s.normal[0], -1)
	e.login(t, s.gid, s.normal[0])
	checkBudgetLifecycle(t, e, s.gid, s.slug, func() string {
		w := e.do("GET", "/"+s.slug+"/ma-cible", "", nil)
		if w.Code != 200 {
			t.Fatalf("carte : %d", w.Code)
		}
		mustContain(t, w.Body.String(), "draw-card-name")
		return w.Body.String()
	})
}

// La version vue est propre à chaque participant et ne recule jamais.
func TestBudgetSeenIsPerParticipantAndMonotonic(t *testing.T) {
	e := newEnv(t)
	s := newScene(t, e, "vu-par-personne", at(48*time.Hour), true)
	e.setBudget(t, s.gid, ptr(2000), 3)
	a, b := s.normal[0], s.normal[1]
	if err := e.store.MarkBudgetSeen(ctx, s.gid, a, 3); err != nil {
		t.Fatal(err)
	}
	if err := e.store.MarkBudgetSeen(ctx, s.gid, a, 1); err != nil {
		t.Fatal(err)
	}
	if v, _ := e.store.BudgetSeen(ctx, s.gid, a); v != 3 {
		t.Errorf("la version vue a reculé : %d", v)
	}
	if v, _ := e.store.BudgetSeen(ctx, s.gid, b); v != 0 {
		t.Errorf("la version vue d'une autre personne : %d", v)
	}
	e.login(t, s.gid, b)
	mustContain(t, e.do("GET", "/"+s.slug+"/participant", "", nil).Body.String(), bannerMark)
}

// Une version plus grande que celle du groupe est ramenée à celle du groupe :
// un budget redéfini plus tard montre encore son bandeau.
func TestBudgetSeenClampedToGroupVersion(t *testing.T) {
	e := newEnv(t)
	s := newScene(t, e, "vu-plafonne", at(48*time.Hour), true)
	e.setBudget(t, s.gid, ptr(2000), 1)
	e.login(t, s.gid, s.normal[0])
	if w := e.post(t, s.slug, "/budget-vu", "", map[string][]string{"version": {"99"}}); w.Code != 303 {
		t.Fatalf("budget-vu : %d", w.Code)
	}
	e.setBudget(t, s.gid, ptr(2200), 2)
	mustContain(t, e.do("GET", "/"+s.slug+"/participant", "", nil).Body.String(), bannerMark)
}
