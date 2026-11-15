package app

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"secretsanta/internal/access/magic"
	accessmig "secretsanta/internal/access/migrations"
	"secretsanta/internal/draw"
	"secretsanta/internal/groups"
	"secretsanta/internal/groupsettings"
	"secretsanta/internal/i18n/cli"
	"secretsanta/internal/letter"
	mailmig "secretsanta/internal/mail/migrations"
	unsubmig "secretsanta/internal/mailunsub/migrations"
	"secretsanta/internal/participants"
	"secretsanta/internal/reveal"
	"secretsanta/internal/rulechange"
	"secretsanta/internal/rules"
	"secretsanta/internal/websec"
)

func get(t *testing.T, a *App, path string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	a.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	return rec
}

func TestRegistryOrderAndEveryModuleApplied(t *testing.T) {
	reg, err := Migrations()
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, m := range reg {
		got = append(got, m.Module)
	}
	want := []string{groups.Module, websec.MigrationModule, magic.Module, participants.Module, rules.Module, accessmig.Module, letter.Module,
		groupsettings.Module, rulechange.Module, draw.Module, mailmig.Module, unsubmig.Module, reveal.Module}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("ordre %v, attendu %v", got, want)
	}
	a := newApp(t)
	for _, m := range want {
		var n int
		if err := a.DB.QueryRow(`SELECT COUNT(*) FROM schema_migrations WHERE module = ?`, m).Scan(&n); err != nil {
			t.Fatal(err)
		}
		if n == 0 {
			t.Errorf("module %s non appliqué", m)
		}
	}
}

func TestServicesBuilt(t *testing.T) {
	a := newApp(t)
	if a.Groups == nil || a.Participants == nil || a.Rules == nil || a.Tokens == nil || a.DKIM == nil {
		t.Fatalf("service manquant : %+v", a)
	}
	if a.Mail != nil {
		t.Error("Mail doit rester nil sans adresse d'expédition")
	}
	if a.DKIM.Enabled() {
		t.Error("DKIM ne doit pas signer sans configuration")
	}
}

func TestHomePage(t *testing.T) {
	a := newApp(t)
	rec := get(t, a, "/")
	if rec.Code != http.StatusOK {
		t.Fatalf("statut %d", rec.Code)
	}
	for _, h := range []string{"Content-Security-Policy", "X-Content-Type-Options", "X-Frame-Options"} {
		if rec.Header().Get(h) == "" {
			t.Errorf("en-tête manquant : %s", h)
		}
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
		t.Errorf("Content-Type %q", ct)
	}
	body := rec.Body.String()
	for _, banned := range []string{"style=", "<style", " onclick="} {
		if strings.Contains(body, banned) {
			t.Errorf("contient %q", banned)
		}
	}
	if !strings.Contains(body, a.Cat.T("fr", "layout.home.title", nil)) {
		t.Errorf("titre français absent : %s", body)
	}
	if get(t, a, "/inconnu").Code != http.StatusNotFound {
		t.Error("chemin inconnu : 404 attendu")
	}
}

func TestStaticCSS(t *testing.T) {
	a := newApp(t)
	rec := get(t, a, "/static/css/base/base.css")
	if rec.Code != http.StatusOK {
		t.Fatalf("statut %d", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/css") {
		t.Errorf("Content-Type %q", ct)
	}
	if rec.Header().Get("Cache-Control") == "" {
		t.Error("Cache-Control manquant")
	}
	if rec.Header().Get("Content-Security-Policy") == "" {
		t.Error("en-têtes de sécurité manquants")
	}
	if get(t, a, "/static/css/base/absent.css").Code != http.StatusNotFound {
		t.Error("fichier absent : 404 attendu")
	}
}

func TestSkeletonAdminOnlyOnLoopback(t *testing.T) {
	for _, u := range []string{"http://localhost:8080", "http://127.0.0.1:8080", "http://[::1]:8080"} {
		a := newAppAt(t, u)
		rec := get(t, a, "/_skeleton/admin")
		if rec.Code != http.StatusOK {
			t.Fatalf("%s : statut %d", u, rec.Code)
		}
		body := rec.Body.String()
		if n := strings.Count(body, `<a class="admin-nav-item"`); n != 5 {
			t.Errorf("%s : %d entrées, attendu 5", u, n)
		}
		if n := strings.Count(body, `aria-current="page"`); n != 1 {
			t.Errorf("%s : %d entrées actives", u, n)
		}
	}
	for _, u := range []string{"https://noel.example.org", "http://noel.example.org", "http://localhost.example.org"} {
		a := newAppAt(t, u)
		if rec := get(t, a, "/_skeleton/admin"); rec.Code != http.StatusNotFound {
			t.Errorf("%s : statut %d, attendu 404", u, rec.Code)
		}
	}
}

func TestPostWithoutCSRFRefused(t *testing.T) {
	a := newApp(t)
	for _, path := range []string{"/", "/_skeleton/admin"} {
		req := httptest.NewRequest(http.MethodPost, path, strings.NewReader("x=1"))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		rec := httptest.NewRecorder()
		a.Handler().ServeHTTP(rec, req)
		if rec.Code != http.StatusForbidden {
			t.Errorf("POST %s : statut %d, attendu 403", path, rec.Code)
		}
	}
}

func TestHealthUnchanged(t *testing.T) {
	a := newApp(t)
	rec := get(t, a, "/health")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "test") {
		t.Fatalf("health : %d %q", rec.Code, rec.Body.String())
	}
}

func TestI18nCheckOnEmbeddedCatalog(t *testing.T) {
	a := newApp(t)
	for _, lang := range []string{"fr", "en"} {
		var out, errb bytes.Buffer
		if code := cli.Run([]string{"check", lang}, &out, &errb, a.Cat); code != 0 {
			t.Errorf("i18n check %s : code %d\n%s%s", lang, code, out.String(), errb.String())
		}
	}
}
