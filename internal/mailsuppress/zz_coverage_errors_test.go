package mailsuppress_test

import (
	"strings"
	"testing"
	"time"

	"secretsanta/internal/db"
	"secretsanta/internal/mailsuppress"
	"secretsanta/internal/shared/clock"
)

func closedStore(t *testing.T) *mailsuppress.Store {
	t.Helper()
	d, err := db.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	d.Close()
	return mailsuppress.New(d, clock.NewFake(time.Date(2026, 11, 1, 10, 0, 0, 0, time.UTC)))
}

func TestStoreErrorsOnUnusableDatabase(t *testing.T) {
	ctx := t.Context()
	s := closedStore(t)
	if _, err := s.IsSuppressed(ctx, 1, "a@example.net"); err == nil {
		t.Error("IsSuppressed doit échouer")
	}
	if err := s.Add(ctx, 1, "a@example.net", "x"); err == nil {
		t.Error("Add doit échouer")
	}
	if err := s.Remove(ctx, 1, "a@example.net"); err == nil {
		t.Error("Remove doit échouer")
	}
	if _, err := s.List(ctx, 1); err == nil {
		t.Error("List doit échouer")
	}
}

func TestAddRefusesEmptyAddressAndTruncatesReason(t *testing.T) {
	ctx := t.Context()
	s, _ := newStore(t)
	if err := s.Add(ctx, 1, "  ", "x"); err == nil {
		t.Fatal("adresse vide refusée")
	}
	if err := s.Add(ctx, 1, "b@example.net", strings.Repeat("é", 500)); err != nil {
		t.Fatal(err)
	}
	l, err := s.List(ctx, 1)
	if err != nil || len(l) != 1 {
		t.Fatalf("%v %v", l, err)
	}
	if n := len([]rune(l[0].Reason)); n > 301 {
		t.Fatalf("raison trop longue : %d", n)
	}
}
