package limiter_test

import (
	"sync"
	"testing"
	"time"

	"secretsanta/internal/access/limiter"
	"secretsanta/internal/shared/clock"
)

// F2b.AC8 : le limiteur compte séparément par adresse IP et par cible.
func TestBucketsCountIPAndTargetSeparately(t *testing.T) {
	clk := clock.NewFake(t0)
	l := limiter.New(clk, limiter.Rule{Limit: 3, Window: 10 * time.Minute})

	// La même chaîne utilisée comme adresse IP et comme cible ne partage pas
	// son compteur.
	same := "203.0.113.5"
	for i := 0; i < 3; i++ {
		if !l.Allow(limiter.IPKey(1, same)).Allowed {
			t.Fatal("refus inattendu sur l'adresse IP")
		}
	}
	if l.Allow(limiter.IPKey(1, same)).Allowed {
		t.Fatal("le seau de l'adresse IP doit être plein")
	}
	for i := 0; i < 3; i++ {
		if !l.Allow(limiter.TargetKey(1, same)).Allowed {
			t.Fatal("le seau de la cible ne doit pas partager le compteur de l'adresse IP")
		}
	}
	if l.Allow(limiter.TargetKey(1, same)).Allowed {
		t.Fatal("le seau de la cible doit être plein à son tour")
	}

	// Deux adresses IP différentes : deux seaux.
	if !l.Allow(limiter.IPKey(1, "203.0.113.6")).Allowed {
		t.Fatal("une autre adresse IP a son propre seau")
	}
	// Deux cibles différentes : deux seaux.
	if !l.Allow(limiter.TargetKey(1, "autre@example.org")).Allowed {
		t.Fatal("une autre cible a son propre seau")
	}
}

// F2b.AC8 : une nouvelle fenêtre s'ouvre après le délai (horloge simulée).
func TestBucketOpensNewWindowAfterDelay(t *testing.T) {
	clk := clock.NewFake(t0)
	rule := limiter.Rule{Limit: 2, Window: time.Hour}
	l := limiter.New(clk, rule)
	k := limiter.TargetKey(3, "zoe@example.org")
	l.Allow(k)
	clk.Advance(50 * time.Minute)
	l.Allow(k)
	d := l.Allow(k)
	if d.Allowed {
		t.Fatal("seau plein : refus attendu")
	}
	// La fenêtre court depuis la première demande (t0) : 10 minutes restantes.
	if d.RetryAfter != 10*time.Minute {
		t.Fatalf("délai attendu 10 min, obtenu %v", d.RetryAfter)
	}
	clk.Advance(10 * time.Minute)
	for i := 0; i < 2; i++ {
		if !l.Allow(k).Allowed {
			t.Fatalf("nouvelle fenêtre : demande %d refusée à tort", i+1)
		}
	}
	if l.Allow(k).Allowed {
		t.Fatal("la nouvelle fenêtre est plafonnée elle aussi")
	}
}

// F2b.AC8 : aucun compteur n'est partagé entre groupes, ni pour l'adresse IP
// ni pour la cible.
func TestNoCounterSharedBetweenGroups(t *testing.T) {
	clk := clock.NewFake(t0)
	l := limiter.New(clk, limiter.Rule{Limit: 1, Window: 15 * time.Minute})
	ip, target := "203.0.113.50", "anne@example.org"
	if !l.Allow(limiter.IPKey(1, ip), limiter.TargetKey(1, target)).Allowed {
		t.Fatal("groupe 1 : première demande refusée")
	}
	if l.Allow(limiter.IPKey(1, ip), limiter.TargetKey(1, target)).Allowed {
		t.Fatal("groupe 1 : seau plein, refus attendu")
	}
	for _, g := range []int64{2, 3, 42} {
		if !l.Allow(limiter.IPKey(g, ip), limiter.TargetKey(g, target)).Allowed {
			t.Fatalf("groupe %d : le compteur du groupe 1 ne doit pas compter ici", g)
		}
	}
}

// Les adresses IPv6 d'un même /64 partagent un seau (un abonné dispose en
// général d'un /64 entier) ; une adresse IPv4 et sa forme IPv6 mappée aussi.
func TestIPKeyNormalisation(t *testing.T) {
	clk := clock.NewFake(t0)
	l := limiter.New(clk, limiter.Rule{Limit: 1, Window: time.Minute})
	if !l.Allow(limiter.IPKey(1, "2001:db8:1:2::1")).Allowed {
		t.Fatal("première demande refusée")
	}
	if l.Allow(limiter.IPKey(1, "2001:db8:1:2:ffff::9")).Allowed {
		t.Fatal("même /64 : même seau attendu")
	}
	if !l.Allow(limiter.IPKey(1, "2001:db8:1:3::1")).Allowed {
		t.Fatal("autre /64 : autre seau attendu")
	}
	if !l.Allow(limiter.IPKey(1, "192.0.2.1")).Allowed {
		t.Fatal("première demande IPv4 refusée")
	}
	if l.Allow(limiter.IPKey(1, "::ffff:192.0.2.1")).Allowed {
		t.Fatal("la forme mappée doit partager le seau IPv4")
	}
	if limiter.IPKey(1, "2001:DB8::1") != limiter.IPKey(1, "2001:db8:0::1") {
		t.Fatal("les écritures d'une même adresse doivent donner la même clé")
	}
}

// Les seaux expirés sont purgés : la mémoire ne croît pas sans fin.
func TestExpiredBucketsArePurged(t *testing.T) {
	clk := clock.NewFake(t0)
	l := limiter.New(clk, limiter.Rule{Limit: 1, Window: time.Minute})
	for i := 0; i < 100; i++ {
		l.Allow(limiter.TargetKey(int64(i), "x"))
	}
	if n := l.Len(); n != 100 {
		t.Fatalf("100 seaux attendus, %d", n)
	}
	clk.Advance(2 * time.Minute)
	l.Allow(limiter.TargetKey(1000, "y"))
	if n := l.Len(); n != 1 {
		t.Fatalf("les seaux expirés doivent être purgés, il en reste %d", n)
	}
}

// Les décisions concurrentes ne dépassent jamais la limite.
func TestConcurrentAllowNeverExceedsLimit(t *testing.T) {
	l := limiter.New(clock.NewFake(t0), limiter.Rule{Limit: 5, Window: time.Minute})
	k := limiter.IPKey(1, "203.0.113.1")
	var wg sync.WaitGroup
	var mu sync.Mutex
	ok := 0
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if l.Allow(k).Allowed {
				mu.Lock()
				ok++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	if ok != 5 {
		t.Fatalf("5 autorisations attendues, %d", ok)
	}
}

func TestInvalidRulePanics(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("une règle invalide doit être refusée")
		}
	}()
	limiter.New(clock.NewFake(t0), limiter.Rule{Limit: 0, Window: time.Minute})
}
