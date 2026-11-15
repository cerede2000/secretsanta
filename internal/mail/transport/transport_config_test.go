package transport

import (
	"context"
	"errors"
	"strings"
	"testing"

	"secretsanta/internal/config"
	"secretsanta/internal/shared/clock"
	"time"
)

// F6.AC1 : la configuration vient des variables d'environnement ; sans SMTP,
// aucun transport (la file passe les messages en échec) et aucun plantage.
func envOf(m map[string]string) func(string) string { return func(k string) string { return m[k] } }

func TestConfigFromEnvironment(t *testing.T) {
	cfg, err := config.Load(envOf(map[string]string{
		"INSTANCE_PASSWORD": "pass-pass-pass", "SIGNING_SECRET": strings.Repeat("s", 40),
		"PUBLIC_URL": "https://noel.example.org",
		"SMTP_HOST":  "smtp.example.org", "SMTP_PORT": "2525", "SMTP_USER": "u", "SMTP_PASSWORD": "p",
		"MAIL_FROM": "noel@example.org", "MAIL_FROM_NAME": "Noël", "MAIL_TLS_MODE": "tls",
		"MAIL_MAX_PER_MINUTE": "30", "MAIL_EHLO_HOSTNAME": "mail.example.org",
	}))
	if err != nil {
		t.Fatal(err)
	}
	tr, err := FromConfig(cfg, nil, clock.Real{})
	if err != nil || tr == nil {
		t.Fatalf("FromConfig: %v %v", tr, err)
	}
	x := tr.(*Transport)
	if x.o.SMTP.Host != "smtp.example.org" || x.o.SMTP.Port != 2525 || x.mode != "tls" ||
		x.o.Mail.MaxPerMinute != 30 || x.o.Mail.EHLOHostname != "mail.example.org" || x.o.SMTP.User != "u" {
		t.Fatalf("configuration non reprise : %+v", x.o)
	}
}

func TestNoSMTPNoTransportNoPanic(t *testing.T) {
	tr, err := FromConfig(&config.Config{}, nil, nil)
	if err != nil || tr != nil {
		t.Fatalf("attendu nil, nil : %v %v", tr, err)
	}
	var nilT *Transport
	if err := nilT.Send(context.Background(), env()); !errors.Is(err, ErrNotConfigured) {
		t.Fatalf("Send sans SMTP : %v", err)
	}
}

func TestPasswordNeverInStrings(t *testing.T) {
	s := newFakeServer(t, "starttls")
	o := baseOpts(s, "starttls")
	o.SMTP.User, o.SMTP.Password = "u", config.NewSecret("tres-secret-123")
	tr, err := New(o)
	if err != nil {
		t.Fatal(err)
	}
	for _, v := range []string{tr.String(), fmtV(tr)} {
		if strings.Contains(v, "tres-secret-123") {
			t.Fatalf("mot de passe exposé : %s", v)
		}
	}
	if err := tr.Send(context.Background(), env()); err != nil {
		t.Fatal(err)
	}
	if len(s.auth) != 1 || !strings.Contains(s.auth[0], "tres-secret-123") {
		t.Fatalf("AUTH : %v", s.auth)
	}
}

func TestRateLimitSpacesSends(t *testing.T) {
	s := newFakeServer(t, "starttls")
	o := baseOpts(s, "starttls")
	o.Mail.MaxPerMinute = 30 // 2 s entre envois
	fc := clock.NewFake(time.Date(2026, 12, 1, 9, 0, 0, 0, time.UTC))
	var slept []time.Duration
	o.Clock = fc
	o.Sleep = func(_ context.Context, d time.Duration) error { slept = append(slept, d); fc.Advance(d); return nil }
	tr, _ := New(o)
	for i := 0; i < 3; i++ {
		if err := tr.Send(context.Background(), env()); err != nil {
			t.Fatal(err)
		}
	}
	if len(slept) != 2 || slept[0] != 2*time.Second {
		t.Fatalf("attentes : %v", slept)
	}
}

func TestSingleConnectionAtATime(t *testing.T) {
	s := newFakeServer(t, "starttls")
	tr, _ := New(baseOpts(s, "starttls"))
	done := make(chan error, 6)
	for i := 0; i < 6; i++ {
		go func() { done <- tr.Send(context.Background(), env()) }()
	}
	for i := 0; i < 6; i++ {
		if err := <-done; err != nil {
			t.Fatal(err)
		}
	}
	if s.maxActive != 1 || len(s.data) != 6 {
		t.Fatalf("connexions simultanées %d, messages %d", s.maxActive, len(s.data))
	}
}

func TestSMTPErrorCodes(t *testing.T) {
	for code, want := range map[string]int{"451 try later": 451, "550 no such user": 550} {
		s := newFakeServer(t, "starttls")
		s.rcptCode = code
		tr, _ := New(baseOpts(s, "starttls"))
		err := tr.Send(context.Background(), env())
		if !hasTextproto(err, want) {
			t.Fatalf("%s : %v", code, err)
		}
	}
}
