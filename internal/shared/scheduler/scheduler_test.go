package scheduler

import (
	"context"
	"errors"
	"testing"
	"time"

	"secretsanta/internal/shared/clock"
)

func TestFiresExactlyAtInstant(t *testing.T) {
	start := time.Date(2026, 12, 1, 8, 0, 0, 0, time.UTC)
	clk := clock.NewFake(start)
	s := New(clk)
	at := start.Add(10 * time.Minute)
	n := 0
	s.At(at, func(context.Context) error { n++; return nil })
	ctx := context.Background()

	clk.Advance(10*time.Minute - time.Nanosecond)
	if c, _ := s.Tick(ctx); c != 0 || n != 0 {
		t.Fatalf("fired early: %d", n)
	}
	clk.Advance(time.Nanosecond)
	if c, _ := s.Tick(ctx); c != 1 || n != 1 {
		t.Fatalf("not fired at instant: %d", n)
	}
	clk.Advance(time.Hour)
	s.Tick(ctx)
	if n != 1 {
		t.Fatalf("fired twice: %d", n)
	}
}

func TestOrderCancelAndError(t *testing.T) {
	start := time.Date(2026, 12, 1, 8, 0, 0, 0, time.UTC)
	clk := clock.NewFake(start)
	s := New(clk)
	var order []int
	boom := errors.New("boom")
	s.At(start.Add(2*time.Second), func(context.Context) error { order = append(order, 2); return boom })
	s.At(start.Add(1*time.Second), func(context.Context) error { order = append(order, 1); return nil })
	id := s.At(start.Add(time.Second), func(context.Context) error { order = append(order, 9); return nil })
	if !s.Cancel(id) || s.Cancel(id) {
		t.Fatal("cancel")
	}
	clk.Advance(time.Minute)
	c, err := s.Tick(context.Background())
	if c != 2 || !errors.Is(err, boom) || len(order) != 2 || order[0] != 1 || order[1] != 2 {
		t.Fatalf("c=%d err=%v order=%v", c, err, order)
	}
	if s.Pending() != 0 {
		t.Fatal("pending")
	}
}
