package mailunsub

import (
	"context"
	"io"
	"log/slog"
	"math/rand/v2"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"secretsanta/internal/config"
	"secretsanta/internal/db"
	"secretsanta/internal/groups"
	"secretsanta/internal/i18n/catalog"
	"secretsanta/internal/i18n/negotiate"
	"secretsanta/internal/mail/templates"
	"secretsanta/internal/mailmsg"
	"secretsanta/internal/mailunsub/migrations"
	"secretsanta/internal/shared/clock"
	"secretsanta/internal/websec"
	"secretsanta/web"
)

const (
	publicURL    = "https://noel.example.org"
	instancePass = "mot-de-passe-instance-correct"
	pid          = int64(42)
)

var t0 = time.Date(2026, 11, 15, 10, 0, 0, 0, time.UTC)

type env struct {
	svc     *Service
	pages   *Pages
	cat     *catalog.Catalog
	group   *groups.Group
	handler http.Handler
}

func realCatalog(t *testing.T) *catalog.Catalog {
	t.Helper()
	cat, err := catalog.Load(web.I18n, catalog.Options{DefaultLang: "fr", Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})
	if err != nil {
		t.Fatal(err)
	}
	return cat
}

func newEnv(t *testing.T) *env {
	t.Helper()
	d, err := db.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = d.Close() })
	gm, err := groups.Migrations()
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Migrate(d, groups.Module, gm); err != nil {
		t.Fatal(err)
	}
	um, err := migrations.Migrations()
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Migrate(d, migrations.Module, um); err != nil {
		t.Fatal(err)
	}
	clk := clock.NewFake(t0)
	gs, err := groups.NewService(d, clk, config.NewSecret(instancePass))
	if err != nil {
		t.Fatal(err)
	}
	res, err := gs.Create(context.Background(), groups.CreateInput{InstancePassword: instancePass, ClientKey: "198.51.100.1", Name: "Noël en famille", Slug: "noel"})
	if err != nil {
		t.Fatal(err)
	}
	g, err := gs.ByID(context.Background(), res.Group.ID)
	if err != nil {
		t.Fatal(err)
	}
	svc, err := NewService(Options{DB: d, Clock: clk, SigningKey: []byte("cle-de-signature-de-test"), PublicURL: publicURL})
	if err != nil {
		t.Fatal(err)
	}
	cat := realCatalog(t)
	pages, err := New(PageOptions{Service: svc, Groups: gs, Translator: cat})
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	pages.Register(mux)
	pol, err := websec.NewPolicy(publicURL)
	if err != nil {
		t.Fatal(err)
	}
	csrf := websec.NewCSRF(pol, websec.CSRFOptions{Exempt: IsOneClickPath}).Middleware(mux)
	e := &env{svc: svc, pages: pages, cat: cat, group: g}
	e.handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		lang := r.Header.Get("X-Test-Lang")
		if lang == "" {
			lang = "fr"
		}
		csrf.ServeHTTP(w, r.WithContext(negotiate.WithLang(r.Context(), lang)))
	})
	return e
}

// link renvoie le chemin signé d'un type pour le participant de test.
func (e *env) link(t *testing.T, typ string) string {
	t.Helper()
	u, err := e.svc.URL(e.group.ID, pid, typ)
	if err != nil {
		t.Fatal(err)
	}
	return strings.TrimPrefix(u, publicURL)
}

// do envoie une requête sans cookie ni jeton CSRF (comme un client de courrier).
func (e *env) do(method, path string, form url.Values, lang string) *httptest.ResponseRecorder {
	var body io.Reader
	if form != nil {
		body = strings.NewReader(form.Encode())
	}
	req := httptest.NewRequest(method, path, body)
	if form != nil {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	req.Header.Set("Sec-Fetch-Site", "cross-site")
	if method == http.MethodGet {
		req.Header.Del("Sec-Fetch-Site")
	}
	req.Header.Set("X-Test-Lang", lang)
	rec := httptest.NewRecorder()
	e.handler.ServeHTTP(rec, req)
	return rec
}

var oneClick = url.Values{"List-Unsubscribe": {"One-Click"}}

func testBuilder(t *testing.T) *mailmsg.Builder {
	t.Helper()
	var seed [32]byte
	seed[0] = 9
	b, err := mailmsg.New(mailmsg.Options{
		PublicURL: publicURL, From: "pere-noel@example.org", FromName: "Père Noël secret",
		Clock: clock.NewFake(t0), Random: rand.NewChaCha8(seed),
	})
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// sample donne des données valides pour un modèle.
func sample(k templates.Kind) templates.Data {
	d := templates.Data{
		To: "lea@example.net", FirstName: "Léa", GroupName: "Famille Martin",
		Link: publicURL + "/famille/p/abc123def456", Code: "K7M2QX",
	}
	switch k {
	case templates.ReadyReception:
		d.Link, d.LetterLink = "", publicURL+"/famille/lettre/abc123def456"
	case templates.Verification:
		d.Link, d.Code = publicURL+"/famille/inscription/confirmer/zz99yy88", "A1B2C3"
	case templates.LetterUpdated:
		d.Ideas = 3
	case templates.BudgetUpdated:
		d.Link = ""
	}
	return d
}

func render(t *testing.T, cat *catalog.Catalog, svc *Service, k templates.Kind, d templates.Data) (mailmsg.Message, string) {
	t.Helper()
	d, err := svc.Decorate(k, 1, pid, d)
	if err != nil {
		t.Fatal(err)
	}
	m, err := templates.Render(cat, k, d)
	if err != nil {
		t.Fatalf("Render(%s) : %v", k, err)
	}
	out, err := testBuilder(t).Build(m)
	if err != nil {
		t.Fatalf("Build(%s) : %v", k, err)
	}
	return m, string(out.Raw)
}
