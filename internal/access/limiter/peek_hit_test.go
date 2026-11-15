package limiter_test

import (
	"testing"
	"time"

	"secretsanta/internal/access/limiter"
	"secretsanta/internal/shared/clock"
)

func TestPeekNeverConsumesAndHitCountsWithoutRefusing(t *testing.T) {
	clk := clock.NewFake(t0)
	l := limiter.New(clk, limiter.Rule{Limit: 2, Window: time.Minute})
	k := limiter.IPKey(1, "203.0.113.9")
	for i := 0; i < 10; i++ {
		if !l.Peek(k).Allowed {
			t.Fatalf("Peek %d : refus alors que rien n'est compté", i)
		}
	}
	if l.Len() != 0 {
		t.Fatalf("Peek ne doit créer aucun seau : %d", l.Len())
	}
	l.Hit(k)
	if !l.Peek(k).Allowed {
		t.Fatal("un seul échec ne remplit pas un seau de 2")
	}
	l.Hit(k)
	d := l.Peek(k)
	if d.Allowed || d.RetryAfter <= 0 {
		t.Fatalf("seau plein : refus avec RetryAfter attendu, %+v", d)
	}
	l.Hit(k) // un seau plein le reste, sans panique
	if l.Peek(k).Allowed {
		t.Fatal("seau toujours plein")
	}
	if l.Peek(limiter.IPKey(1, "203.0.113.10")).Allowed != true {
		t.Fatal("une autre clé reste libre")
	}
	if l.Peek().Allowed {
		t.Fatal("sans clé : refus")
	}
	clk.Advance(time.Minute + time.Second)
	if !l.Peek(k).Allowed {
		t.Fatal("la fenêtre écoulée rouvre le seau")
	}
}
