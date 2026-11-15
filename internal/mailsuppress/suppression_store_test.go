package mailsuppress_test

import (
	"strings"
	"testing"
	"time"

	"secretsanta/internal/db"
	"secretsanta/internal/mailsuppress"
	"secretsanta/internal/shared/clock"
)

func newStore(t *testing.T) (*mailsuppress.Store, *clock.Fake) {
	t.Helper()
	d, err := db.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	ms, err := mailsuppress.Migrations()
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Migrate(d, mailsuppress.Module, ms); err != nil {
		t.Fatal(err)
	}
	clk := clock.NewFake(time.Date(2026, 11, 1, 10, 0, 0, 0, time.UTC))
	return mailsuppress.New(d, clk), clk
}

func TestPermanentRefusalIsPerGroupListedAndRemovable(t *testing.T) {
	ctx := t.Context()
	s, clk := newStore(t)
	if err := s.Add(ctx, 1, "Lea@Example.net", "550 5.1.1 user\tunknown\r\n"); err != nil {
		t.Fatal(err)
	}
	if ok, _ := s.IsSuppressed(ctx, 1, " lea@example.net "); !ok {
		t.Fatal("supprimée attendue pour le groupe 1")
	}
	if ok, _ := s.IsSuppressed(ctx, 2, "lea@example.net"); ok {
		t.Fatal("le groupe 2 ne doit pas être concerné")
	}
	// Adresse corrigée : pas supprimée.
	if ok, _ := s.IsSuppressed(ctx, 1, "lea.corrigee@example.net"); ok {
		t.Fatal("une adresse corrigée ne doit pas être supprimée")
	}
	l1, err := s.List(ctx, 1)
	if err != nil || len(l1) != 1 {
		t.Fatalf("liste du groupe 1 : %v %v", l1, err)
	}
	e := l1[0]
	if e.Address != "lea@example.net" || e.Reason != "550 5.1.1 user unknown" || !e.At.Equal(clk.Now()) {
		t.Fatalf("entrée inattendue : %+v", e)
	}
	if l2, _ := s.List(ctx, 2); len(l2) != 0 {
		t.Fatalf("groupe 2 : liste vide attendue, reçu %v", l2)
	}
	// Une autre inscription ailleurs ne touche pas le groupe 1 au retrait.
	_ = s.Add(ctx, 2, "lea@example.net", "x")
	if err := s.Remove(ctx, 1, "LEA@example.net"); err != nil {
		t.Fatal(err)
	}
	if ok, _ := s.IsSuppressed(ctx, 1, "lea@example.net"); ok {
		t.Fatal("retrait attendu")
	}
	if ok, _ := s.IsSuppressed(ctx, 2, "lea@example.net"); !ok {
		t.Fatal("le groupe 2 reste supprimé")
	}
	if err := s.Remove(ctx, 1, "absente@example.net"); err != nil {
		t.Fatalf("retrait d'une absente : %v", err)
	}
}

func TestReAddUpdatesReasonAndDate(t *testing.T) {
	ctx := t.Context()
	s, clk := newStore(t)
	_ = s.Add(ctx, 1, "a@x.fr", "first")
	clk.Advance(time.Hour)
	_ = s.Add(ctx, 1, "A@x.fr", "second")
	l, _ := s.List(ctx, 1)
	if len(l) != 1 || l[0].Reason != "second" || !l[0].At.Equal(clk.Now()) {
		t.Fatalf("%+v", l)
	}
}

func TestReasonIsTruncatedAndEmptyAddressRefused(t *testing.T) {
	ctx := t.Context()
	s, _ := newStore(t)
	if err := s.Add(ctx, 1, "  ", "x"); err == nil {
		t.Fatal("adresse vide refusée")
	}
	_ = s.Add(ctx, 1, "a@x.fr", strings.Repeat("é", 1000))
	l, _ := s.List(ctx, 1)
	if n := len([]rune(l[0].Reason)); n != 301 {
		t.Fatalf("raison plafonnée attendue, %d", n)
	}
}
