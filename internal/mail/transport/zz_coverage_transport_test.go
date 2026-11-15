package transport

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestCoverageSleepCtx(t *testing.T) {
	if err := sleepCtx(context.Background(), time.Millisecond); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := sleepCtx(ctx, time.Minute); !errors.Is(err, context.Canceled) {
		t.Fatalf("attendu Canceled, reçu %v", err)
	}
}

func TestCoveragePlainAuthNext(t *testing.T) {
	a := plainAuth{user: "u"}
	if _, err := a.Next(nil, true); err == nil {
		t.Fatal("réponse inattendue acceptée")
	}
	if b, err := a.Next(nil, false); err != nil || b != nil {
		t.Fatalf("%v %v", b, err)
	}
}

func TestCoverageRedactErr(t *testing.T) {
	base := errors.New("535 mauvais mot de passe hunter2")
	if got := redactErr(base, "hunter2"); strings.Contains(got.Error(), "hunter2") || !strings.Contains(got.Error(), "535") {
		t.Fatalf("non expurgé : %v", got)
	}
	if got := redactErr(base, ""); got != base {
		t.Fatal("secret vide doit laisser l'erreur")
	}
	if got := redactErr(base, "absent"); got != base {
		t.Fatal("secret absent doit laisser l'erreur")
	}
	if redactErr(nil, "x") != nil {
		t.Fatal("nil attendu")
	}
}

func TestCoverageCheckHeaders(t *testing.T) {
	ok := "From: a@b.test\r\nSubject: x\r\n continued\r\n\r\ncorps"
	if err := checkHeaders([]byte(ok)); err != nil {
		t.Fatal(err)
	}
	bad := []string{
		"From: a@b.test",
		"From: a\x00b\r\n\r\nx",
		"Mauvaise ligne\r\n\r\nx",
		"Bad Name: v\r\n\r\nx",
		": v\r\n\r\nx",
		"From: a\rb\r\n\r\nx",
	}
	for _, b := range bad {
		if checkHeaders([]byte(b)) == nil {
			t.Errorf("en-tête accepté à tort : %q", b)
		}
	}
}
