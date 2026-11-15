package queue_test

import (
	"fmt"
	"testing"
	"time"

	"secretsanta/internal/mail/queue"
)

func TestDelayBetweenSends(t *testing.T) {
	e := newEnv(t, func(o *queue.Options) { o.Delay = 5 * time.Second })
	for i := 0; i < 3; i++ {
		mustEnqueue(t, e, item(fmt.Sprintf("p%d@example.net", i), "Ana", int64(i+1)))
	}
	if n := tick(t, e); n != 1 {
		t.Fatalf("un seul envoi avant le délai, obtenu %d", n)
	}
	e.Clk.Advance(4 * time.Second)
	if tick(t, e) != 0 {
		t.Fatal("délai de 5 s non respecté")
	}
	e.Clk.Advance(time.Second)
	if tick(t, e) != 1 {
		t.Fatal("envoi attendu après 5 s")
	}
	e.Clk.Advance(5 * time.Second)
	if tick(t, e) != 1 || e.Tr.count() != 3 {
		t.Fatal("troisième envoi attendu")
	}
	for i := 1; i < len(e.Tr.Times); i++ {
		if e.Tr.Times[i].Sub(e.Tr.Times[i-1]) < 5*time.Second {
			t.Fatalf("écart trop court : %v", e.Tr.Times)
		}
	}
}

func TestPerMinuteLimit(t *testing.T) {
	e := newEnv(t, func(o *queue.Options) { o.MaxPerMinute = 2 })
	for i := 0; i < 5; i++ {
		mustEnqueue(t, e, item(fmt.Sprintf("p%d@example.net", i), "Ana", int64(i+1)))
	}
	if n := tick(t, e); n != 2 {
		t.Fatalf("2 envois par minute attendus, obtenu %d", n)
	}
	e.Clk.Advance(59 * time.Second)
	if tick(t, e) != 0 {
		t.Fatal("limite par minute dépassée")
	}
	e.Clk.Advance(time.Second)
	if n := tick(t, e); n != 2 {
		t.Fatalf("2 envois attendus à la minute suivante, obtenu %d", n)
	}
	e.Clk.Advance(time.Minute)
	if tick(t, e) != 1 || e.Tr.count() != 5 {
		t.Fatalf("dernier envoi attendu, total %d", e.Tr.count())
	}
}
