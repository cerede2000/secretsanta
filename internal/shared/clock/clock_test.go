package clock

import (
	"testing"
	"time"
)

func TestFake(t *testing.T) {
	start := time.Date(2026, 12, 1, 8, 0, 0, 0, time.UTC)
	f := NewFake(start)
	var c Clock = f
	if !c.Now().Equal(start) {
		t.Fatal("start")
	}
	f.Advance(time.Hour)
	if !c.Now().Equal(start.Add(time.Hour)) {
		t.Fatal("advance")
	}
	f.Set(start)
	if !c.Now().Equal(start) {
		t.Fatal("set")
	}
	if Real.Now(Real{}).IsZero() {
		t.Fatal("real")
	}
}
