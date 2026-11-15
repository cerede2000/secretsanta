package app

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"secretsanta/internal/config"
	"secretsanta/internal/db"
	"secretsanta/internal/shared/clock"
	"secretsanta/internal/websec"
)

func newApp(t *testing.T) *App { return newAppAt(t, "http://localhost:8080") }

func newAppAt(t *testing.T, publicURL string) *App {
	t.Helper()
	d, err := db.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	cfg := config.Config{
		PublicURL:    publicURL,
		InstancePass: config.NewSecret("mot-de-passe-d-instance-test"),
		SigningKey:   config.NewSecret("secret-de-signature-de-test"),
	}
	a, err := New(cfg, d, clock.NewFake(time.Date(2026, 11, 1, 10, 0, 0, 0, time.UTC)), "test")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(a.Close)
	return a
}

func TestMigrationsApplyAndReplay(t *testing.T) {
	a := newApp(t)
	count := func() int {
		var n int
		if err := a.DB.QueryRow(`SELECT COUNT(*) FROM schema_migrations`).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	before := count()
	if before == 0 {
		t.Fatal("aucune migration appliquée")
	}
	if err := Migrate(a.DB); err != nil {
		t.Fatalf("rejeu : %v", err)
	}
	if after := count(); after != before {
		t.Fatalf("le rejeu a changé le suivi : %d -> %d", before, after)
	}
}

func TestMigrationRegistry(t *testing.T) {
	reg, err := Migrations()
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for _, m := range reg {
		if seen[m.Module] {
			t.Fatalf("module en double : %s", m.Module)
		}
		seen[m.Module] = true
		if len(m.Migrations) == 0 {
			t.Errorf("module %s sans migration", m.Module)
		}
	}
	for _, want := range []string{"groups", "websec", "access_magic", "participants", "rules", "access_credentials"} {
		if !seen[want] {
			t.Errorf("module manquant : %s", want)
		}
	}
}

func TestCSRFRejectsPostWithFrenchText(t *testing.T) {
	a := newApp(t)
	texts, err := LoadTexts("fr")
	if err != nil {
		t.Fatal(err)
	}
	if texts.Text("fr", "core.csrf.reject") != websec.CSRFRejectMessage {
		t.Fatal("fr.json diverge de websec.CSRFRejectMessage")
	}
	req := httptest.NewRequest(http.MethodPost, "/health", strings.NewReader("x=1"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	a.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("statut %d, attendu 403", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), texts.Text("fr", "core.csrf.reject")) {
		t.Fatalf("texte inattendu : %q", rec.Body.String())
	}
}

func TestHealthAndSecurityHeaders(t *testing.T) {
	a := newApp(t)
	rec := httptest.NewRecorder()
	a.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/health", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("statut %d, attendu 200", rec.Code)
	}
	for _, h := range []string{"Content-Security-Policy", "X-Content-Type-Options"} {
		if rec.Header().Get(h) == "" {
			t.Errorf("en-tête manquant : %s", h)
		}
	}
}

func TestEveryRunsAndReschedules(t *testing.T) {
	a := newApp(t)
	n := 0
	a.Every("test", time.Minute, func(_ context.Context) error { n++; return nil })
	fake := a.Clock.(*clock.Fake)
	for i := 0; i < 2; i++ {
		fake.Advance(time.Minute)
		if _, err := a.sched.Tick(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	if n != 2 {
		t.Fatalf("exécutions %d, attendu 2", n)
	}
}
