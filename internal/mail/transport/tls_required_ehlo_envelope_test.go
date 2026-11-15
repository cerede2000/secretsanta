package transport

import (
	"context"
	"errors"
	"testing"
)

// F6.AC22 : chiffrement exigé par défaut, EHLO et expéditeur d'enveloppe configurés.
func TestEncryptionRequiredByDefault(t *testing.T) {
	s := newFakeServer(t, "none")
	o := baseOpts(s, "") // mode vide = STARTTLS par défaut
	tr, err := New(o)
	if err != nil {
		t.Fatal(err)
	}
	if tr.mode != "starttls" {
		t.Fatalf("mode par défaut %q", tr.mode)
	}
	if err := tr.Send(context.Background(), env()); err == nil {
		t.Fatal("serveur sans STARTTLS accepté par défaut")
	}
	if len(s.data) != 0 {
		t.Fatal("message parti en clair")
	}
}

func TestNoneRefusedWithoutAllowInsecure(t *testing.T) {
	s := newFakeServer(t, "none")
	if _, err := New(baseOpts(s, "none")); !errors.Is(err, ErrInsecureRefused) {
		t.Fatalf("attendu ErrInsecureRefused : %v", err)
	}
	o := baseOpts(s, "none")
	o.Mail.AllowInsecure = true
	if _, err := New(o); err != nil {
		t.Fatalf("none explicite refusé : %v", err)
	}
}

func TestEHLOAndEnvelopeFrom(t *testing.T) {
	s := newFakeServer(t, "starttls")
	tr, _ := New(baseOpts(s, "starttls"))
	if err := tr.Send(context.Background(), env()); err != nil {
		t.Fatal(err)
	}
	if len(s.ehlo) == 0 || s.ehlo[0] != "mail.example.org" {
		t.Fatalf("EHLO : %v", s.ehlo)
	}
	if len(s.mailFrom) != 1 || s.mailFrom[0] != "bounce@example.org" {
		t.Fatalf("MAIL FROM : %v", s.mailFrom)
	}
	if len(s.rcptTo) != 1 || s.rcptTo[0] != "lea@example.net" {
		t.Fatalf("RCPT TO : %v", s.rcptTo)
	}
}

func TestEnvelopeFromFallsBackToMessageSender(t *testing.T) {
	s := newFakeServer(t, "starttls")
	o := baseOpts(s, "starttls")
	o.Mail.EnvelopeFrom = ""
	tr, _ := New(o)
	if err := tr.Send(context.Background(), env()); err != nil {
		t.Fatal(err)
	}
	if s.mailFrom[0] != "noel@example.org" {
		t.Fatalf("MAIL FROM : %v", s.mailFrom)
	}
}
