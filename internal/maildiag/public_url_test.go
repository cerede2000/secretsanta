package maildiag

import (
	"context"
	"crypto/tls"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func publicDiag(t *testing.T, srv *httptest.Server, trust bool) *Diagnostic {
	t.Helper()
	cfg := baseConfig()
	cfg.PublicURL = srv.URL
	cfg.SMTP.Host = ""
	o := Options{Config: cfg, Resolver: newResolver(), Timeout: 3 * time.Second}
	if trust {
		o.Client = srv.Client()
	}
	return New(o)
}

func TestPublicURLHTTPSAndHealth(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/health" {
			_, _ = w.Write([]byte("ok"))
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()
	checks := publicDiag(t, srv, true).checkPublicURL(context.Background())
	if len(checks) != 2 || checks[0].Code != "public_https.ok" || checks[1].Code != "public_health.ok" {
		t.Fatalf("%+v", checks)
	}
}

func TestPublicURLBadHealth(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(503) }))
	defer srv.Close()
	checks := publicDiag(t, srv, true).checkPublicURL(context.Background())
	if checks[1].Code != "public_health.bad_status" || checks[1].Params["status"] != "503" || checks[1].Status != StatusError {
		t.Fatalf("%+v", checks)
	}
}

func TestPublicURLUntrustedCertificate(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer srv.Close()
	checks := publicDiag(t, srv, false).checkPublicURL(context.Background())
	if checks[0].Code != "public_https.untrusted" || checks[0].Status != StatusError {
		t.Fatalf("%+v", checks)
	}
	if checks[1].Code != "public_health.unreachable" {
		t.Fatalf("%+v", checks)
	}
}

func TestPublicURLExpiredAndHostname(t *testing.T) {
	p := newPKI(t)
	for name, c := range map[string]struct {
		ip   string
		end  time.Time
		code string
	}{
		"expired":  {"127.0.0.1", time.Now().Add(-time.Hour), "public_https.cert_expired"},
		"hostname": {"10.9.9.9", time.Now().Add(time.Hour), "public_https.hostname"},
	} {
		t.Run(name, func(t *testing.T) {
			srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
			srv.TLS = &tls.Config{Certificates: []tls.Certificate{p.leaf(t, c.ip, c.end)}}
			srv.StartTLS()
			defer srv.Close()
			cfg := baseConfig()
			cfg.PublicURL = srv.URL
			rt := &http.Transport{TLSClientConfig: &tls.Config{RootCAs: p.pool}}
			cl := srv.Client()
			cl.Transport = rt
			got := New(Options{Config: cfg, Resolver: newResolver(), Client: cl}).checkPublicURL(context.Background())
			if got[0].Code != c.code {
				t.Fatalf("%+v", got)
			}
		})
	}
}

func TestPublicURLPlainHTTP(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer srv.Close()
	// 127.0.0.1 : sans objet, mais /health est contrôlé.
	checks := publicDiag(t, srv, false).checkPublicURL(context.Background())
	if checks[0].Code != "public_https.local" || checks[0].Status != StatusOff || checks[1].Code != "public_health.ok" {
		t.Fatalf("%+v", checks)
	}
	cfg := baseConfig()
	cfg.PublicURL = "http://noel.example.org"
	d := New(Options{Config: cfg, Resolver: newResolver(), Client: &http.Client{Transport: failTransport{}}})
	got := d.checkPublicURL(context.Background())
	if got[0].Code != "public_https.not_https" || got[0].Status != StatusWarn {
		t.Fatalf("%+v", got)
	}
}

type failTransport struct{}

func (failTransport) RoundTrip(*http.Request) (*http.Response, error) {
	return nil, http.ErrHandlerTimeout
}
