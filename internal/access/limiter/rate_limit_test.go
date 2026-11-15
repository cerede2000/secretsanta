package limiter_test

import (
	"fmt"
	"testing"
	"time"

	"secretsanta/internal/access/limiter"
	"secretsanta/internal/shared/clock"
)

var t0 = time.Date(2026, 12, 1, 10, 0, 0, 0, time.UTC)

// F2b.AC7 : la 6e demande de lien magique est bloquée dans la fenêtre de
// 15 minutes, puis réautorisée après la fenêtre (horloge simulée).
func TestMagicLinkRequestsLimitedToFivePerWindow(t *testing.T) {
	for _, tc := range []struct {
		name string
		keys func(i int) []limiter.Key
	}{
		{"même adresse IP et même cible", func(int) []limiter.Key {
			return []limiter.Key{limiter.IPKey(1, "203.0.113.7"), limiter.TargetKey(1, "anne@example.org")}
		}},
		{"même adresse IP, cibles différentes", func(i int) []limiter.Key {
			return []limiter.Key{limiter.IPKey(1, "203.0.113.7"), limiter.TargetKey(1, fmt.Sprintf("p%d@example.org", i))}
		}},
		{"même cible, adresses IP différentes", func(i int) []limiter.Key {
			return []limiter.Key{limiter.IPKey(1, fmt.Sprintf("198.51.100.%d", i+1)), limiter.TargetKey(1, "anne@example.org")}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			clk := clock.NewFake(t0)
			l := limiter.New(clk, limiter.MagicLinkRequests)
			if limiter.MagicLinkRequests.Limit != 5 || limiter.MagicLinkRequests.Window != 15*time.Minute {
				t.Fatalf("règle du lien magique inattendue : %+v", limiter.MagicLinkRequests)
			}
			for i := 0; i < 5; i++ {
				if d := l.Allow(tc.keys(i)...); !d.Allowed {
					t.Fatalf("demande %d refusée à tort", i+1)
				}
				clk.Advance(time.Minute)
			}
			d := l.Allow(tc.keys(5)...)
			if d.Allowed {
				t.Fatal("la 6e demande dans la fenêtre doit être bloquée")
			}
			if d.RetryAfter <= 0 || d.RetryAfter > 15*time.Minute {
				t.Fatalf("délai avant nouvel essai incohérent : %v", d.RetryAfter)
			}
			// Toujours dans la fenêtre (1 ns avant sa fin) : encore bloqué.
			clk.Set(t0.Add(15*time.Minute - time.Nanosecond))
			if l.Allow(tc.keys(6)...).Allowed {
				t.Fatal("encore dans la fenêtre : doit rester bloqué")
			}
			// Fenêtre écoulée : réautorisé.
			clk.Set(t0.Add(15 * time.Minute))
			if !l.Allow(tc.keys(7)...).Allowed {
				t.Fatal("après la fenêtre, la demande doit être réautorisée")
			}
		})
	}
}

// F2b.AC7 : la 11e tentative de code est bloquée dans la fenêtre de
// 15 minutes, puis réautorisée après la fenêtre (horloge simulée).
func TestCodeAttemptsLimitedToTenPerWindow(t *testing.T) {
	if limiter.CodeAttempts.Limit != 10 || limiter.CodeAttempts.Window != 15*time.Minute {
		t.Fatalf("règle des tentatives de code inattendue : %+v", limiter.CodeAttempts)
	}
	clk := clock.NewFake(t0)
	l := limiter.New(clk, limiter.CodeAttempts)
	keys := []limiter.Key{limiter.IPKey(7, "2001:db8::1"), limiter.TargetKey(7, "connexion-code")}
	for i := 0; i < 10; i++ {
		if !l.Allow(keys...).Allowed {
			t.Fatalf("tentative %d refusée à tort", i+1)
		}
		clk.Advance(30 * time.Second)
	}
	if l.Allow(keys...).Allowed {
		t.Fatal("la 11e tentative dans la fenêtre doit être bloquée")
	}
	clk.Set(t0.Add(15 * time.Minute))
	for i := 0; i < 10; i++ {
		if !l.Allow(keys...).Allowed {
			t.Fatalf("après la fenêtre, tentative %d refusée à tort", i+1)
		}
	}
	if l.Allow(keys...).Allowed {
		t.Fatal("la nouvelle fenêtre est elle aussi plafonnée à 10")
	}
}

// Une demande refusée ne consomme aucun crédit : le blocage d'une clé ne
// fait pas avancer les compteurs des autres clés de la même demande.
func TestRefusalDoesNotCountOnOtherKeys(t *testing.T) {
	clk := clock.NewFake(t0)
	l := limiter.New(clk, limiter.Rule{Limit: 2, Window: time.Minute})
	ip := limiter.IPKey(1, "203.0.113.9")
	for i := 0; i < 2; i++ {
		if !l.Allow(ip, limiter.TargetKey(1, "a")).Allowed {
			t.Fatal("refus inattendu")
		}
	}
	// L'adresse IP est épuisée : la cible « b » n'est pas débitée.
	for i := 0; i < 5; i++ {
		if l.Allow(ip, limiter.TargetKey(1, "b")).Allowed {
			t.Fatal("l'adresse IP épuisée doit bloquer")
		}
	}
	if !l.Allow(limiter.IPKey(1, "198.51.100.1"), limiter.TargetKey(1, "b")).Allowed {
		t.Fatal("la cible « b » ne doit pas avoir été débitée par les refus")
	}
}

func TestAllowWithoutKeysIsRefused(t *testing.T) {
	l := limiter.New(clock.NewFake(t0), limiter.CodeAttempts)
	if l.Allow().Allowed {
		t.Fatal("une demande sans clé doit être refusée (configuration erronée)")
	}
}
