package maildiag

import (
	"context"
	"crypto/tls"
	"testing"
	"time"

	"secretsanta/internal/config"
)

func tlsCase(t *testing.T, serverMode, cfgMode string, cert func(p *pki) tls.Certificate) Check {
	t.Helper()
	p := newPKI(t)
	srv := startSMTP(t, serverMode, cert(p))
	cfg := baseConfig()
	cfg.SMTP.Host = "127.0.0.1"
	cfg.SMTP.Port = srv.port()
	cfg.Mail.TLSMode = cfgMode
	cfg.Mail.AllowInsecure = cfgMode == config.TLSNone
	d := New(Options{Config: cfg, Resolver: newResolver(), TLSConfig: &tls.Config{RootCAs: p.pool}, Timeout: 3 * time.Second})
	return d.checkSMTPTLS(context.Background())
}

func TestSMTPTLS(t *testing.T) {
	valid := func(tt *testing.T) func(p *pki) tls.Certificate {
		return func(p *pki) tls.Certificate { return p.leaf(tt, "127.0.0.1", time.Now().Add(24*time.Hour)) }
	}
	expired := func(tt *testing.T) func(p *pki) tls.Certificate {
		return func(p *pki) tls.Certificate { return p.leaf(tt, "127.0.0.1", time.Now().Add(-24*time.Hour)) }
	}
	wrongHost := func(tt *testing.T) func(p *pki) tls.Certificate {
		return func(p *pki) tls.Certificate { return p.leaf(tt, "10.9.9.9", time.Now().Add(24*time.Hour)) }
	}
	cases := []struct {
		name         string
		server, mode string
		cert         func(*testing.T) func(*pki) tls.Certificate
		status       Status
		code         string
	}{
		{"starttls valide", "starttls", config.TLSStartTLS, valid, StatusOK, "smtp_tls.ok"},
		{"tls valide", "implicit", config.TLSImplicit, valid, StatusOK, "smtp_tls.ok"},
		{"starttls expiré", "starttls", config.TLSStartTLS, expired, StatusError, "smtp_tls.cert_expired"},
		{"tls expiré", "implicit", config.TLSImplicit, expired, StatusError, "smtp_tls.cert_expired"},
		{"starttls nom incorrect", "starttls", config.TLSStartTLS, wrongHost, StatusError, "smtp_tls.hostname"},
		{"tls nom incorrect", "implicit", config.TLSImplicit, wrongHost, StatusError, "smtp_tls.hostname"},
		{"starttls absent", "plain", config.TLSStartTLS, valid, StatusError, "smtp_tls.no_encryption"},
		{"mode none", "plain", config.TLSNone, valid, StatusWarn, "smtp_tls.none_configured"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := tlsCase(t, c.server, c.mode, c.cert(t))
			if got.Status != c.status || got.Code != c.code {
				t.Fatalf("obtenu %s/%s, attendu %s/%s", got.Status, got.Code, c.status, c.code)
			}
		})
	}
}

func TestSMTPTLSUntrustedAndUnreachable(t *testing.T) {
	p := newPKI(t)
	srv := startSMTP(t, "starttls", p.leaf(t, "127.0.0.1", time.Now().Add(time.Hour)))
	cfg := baseConfig()
	cfg.SMTP.Host, cfg.SMTP.Port = "127.0.0.1", srv.port()
	got := New(Options{Config: cfg, Resolver: newResolver(), Timeout: 3 * time.Second}).checkSMTPTLS(context.Background())
	if got.Code != "smtp_tls.untrusted" {
		t.Fatalf("%+v", got)
	}
	srv.ln.Close()
	got = New(Options{Config: cfg, Resolver: newResolver(), Timeout: time.Second}).checkSMTPTLS(context.Background())
	if got.Code != "smtp_tls.unreachable" {
		t.Fatalf("%+v", got)
	}
}
