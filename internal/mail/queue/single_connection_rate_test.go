package queue_test

import (
	"fmt"
	"sync"
	"testing"
	"time"

	"secretsanta/internal/mail/queue"
)

func TestSingleConnectionAtATime(t *testing.T) {
	e := newEnv(t, nil)
	e.Tr.Hold = 5 * time.Millisecond
	for i := 0; i < 12; i++ {
		mustEnqueue(t, e, item(fmt.Sprintf("p%d@example.net", i), "Ana", int64(i+1)))
	}
	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); _, _ = e.Svc.Tick(t.Context()) }()
	}
	wg.Wait()
	if got := e.Tr.MaxSeen.Load(); got != 1 {
		t.Fatalf("une connexion à la fois attendue, %d en parallèle", got)
	}
	if e.Tr.count() != 12 {
		t.Fatalf("12 envois attendus, %d", e.Tr.count())
	}
}

func TestRateCapAcrossMinutes(t *testing.T) {
	e := newEnv(t, func(o *queue.Options) { o.MaxPerMinute = 3 })
	for i := 0; i < 9; i++ {
		mustEnqueue(t, e, item(fmt.Sprintf("p%d@example.net", i), "Ana", int64(i+1)))
	}
	for minute := 0; minute < 3; minute++ {
		if n := tick(t, e); n != 3 {
			t.Fatalf("minute %d : 3 envois attendus, %d", minute, n)
		}
		e.Clk.Advance(time.Minute)
	}
	// Dans toute fenêtre glissante d'une minute, au plus 3 envois.
	ts := e.Tr.Times
	for i := range ts {
		n := 0
		for _, u := range ts {
			if !u.Before(ts[i]) && u.Sub(ts[i]) < time.Minute {
				n++
			}
		}
		if n > 3 {
			t.Fatalf("%d envois dans une minute", n)
		}
	}
}
