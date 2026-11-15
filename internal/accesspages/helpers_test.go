package accesspages

import (
	"context"
	"database/sql"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"secretsanta/internal/access/magic"
	"secretsanta/internal/access/token"
	"secretsanta/internal/config"
	"secretsanta/internal/db"
	"secretsanta/internal/groups"
	"secretsanta/internal/i18n/catalog"
	"secretsanta/internal/i18n/negotiate"
	"secretsanta/internal/shared/clock"
	"secretsanta/internal/shared/errs"
	"secretsanta/internal/websec"
	"secretsanta/web"
)

const (
	instancePass = "mot-de-passe-instance-correct"
	publicURL    = "http://127.0.0.1:8080"
)

var t0 = time.Date(2026, 11, 15, 10, 0, 0, 0, time.UTC)

// fakeSessions enregistre les sessions ouvertes.
type fakeSessions struct {
	mu     sync.Mutex
	opened []websec.Identity
}

func (f *fakeSessions) Create(_ context.Context, w http.ResponseWriter, _ *http.Request, id websec.Identity) (*websec.Session, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.opened = append(f.opened, id)
	http.SetCookie(w, &http.Cookie{Name: "test_" + string(id.Kind), Value: "x"})
	return &websec.Session{Identity: id}, nil
}

func (f *fakeSessions) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.opened)
}

// fakeMagic simule le service de lien magique.
type fakeMagic struct {
	mu       sync.Mutex
	requests []magic.Request
	valid    map[string]int64 // jeton -> participant
	limited  bool
}

func (f *fakeMagic) RequestLink(_ context.Context, req magic.Request) (magic.Response, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if !strings.Contains(req.Email, "@") {
		return magic.Response{}, errs.Validation("adresse")
	}
	if f.limited {
		return magic.Response{}, limiterError()
	}
	f.requests = append(f.requests, req)
	return magic.UniformResponse, nil
}

func (f *fakeMagic) Consume(_ context.Context, groupID int64, tok string) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	pid, ok := f.valid[tok]
	if !ok {
		return 0, errs.New(errs.KindUnauthorized, "lien")
	}
	delete(f.valid, tok)
	return pid, nil
}

type env struct {
	db       *sql.DB
	groups   *groups.Service
	creds    *token.Service
	magic    *fakeMagic
	sessions *fakeSessions
	clock    *clock.Fake
	pages    *Pages
	handler  http.Handler
	cat      *catalog.Catalog
	anchor   string // groupe connu où prendre le jeton CSRF
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
	ms, err := groups.Migrations()
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Migrate(d, groups.Module, ms); err != nil {
		t.Fatal(err)
	}
	clk := clock.NewFake(t0)
	gs, err := groups.NewService(d, clk, config.NewSecret(instancePass))
	if err != nil {
		t.Fatal(err)
	}
	creds, err := token.NewService(token.NewMemoryStore(), token.LoginCodeLength, []byte("cle-de-signature-de-test"))
	if err != nil {
		t.Fatal(err)
	}
	e := &env{db: d, groups: gs, creds: creds, clock: clk, magic: &fakeMagic{valid: map[string]int64{}}, sessions: &fakeSessions{}, cat: realCatalog(t)}
	e.pages, err = New(Options{Groups: gs, Credentials: creds, Magic: e.magic, Sessions: e.sessions, Translator: e.cat, Clock: clk})
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	e.pages.Register(mux)
	pol, err := websec.NewPolicy(publicURL)
	if err != nil {
		t.Fatal(err)
	}
	csrf := websec.NewCSRF(pol, websec.CSRFOptions{}).Middleware(mux)
	e.handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		lang := r.Header.Get("X-Test-Lang")
		if lang == "" {
			lang = "fr"
		}
		csrf.ServeHTTP(w, r.WithContext(negotiate.WithLang(r.Context(), lang)))
	})
	return e
}

func (e *env) createGroup(t *testing.T, slug string) *groups.CreateResult {
	t.Helper()
	r, err := e.groups.Create(context.Background(), groups.CreateInput{InstancePassword: instancePass, ClientKey: "198.51.100.1", Name: "Noël en famille", Slug: slug})
	if err != nil {
		t.Fatal(err)
	}
	if e.anchor == "" {
		e.anchor = slug
	}
	return r
}

var csrfRe = regexp.MustCompile(`name="csrf_token" value="([^"]+)"`)

// do envoie une requête ; les POST portent le jeton et le cookie CSRF d'une
// visite préalable de la même page.
func (e *env) do(t *testing.T, method, target string, form url.Values, ip, lang string) *httptest.ResponseRecorder {
	t.Helper()
	var cookies []*http.Cookie
	if method == http.MethodPost {
		get := httptest.NewRequest(http.MethodGet, "/"+e.anchor+"/connexion", nil)
		get.RemoteAddr = ip + ":1234"
		rec := httptest.NewRecorder()
		e.handler.ServeHTTP(rec, get)
		m := csrfRe.FindStringSubmatch(rec.Body.String())
		if m == nil {
			t.Fatalf("jeton CSRF introuvable : %d %s", rec.Code, rec.Body.String())
		}
		cookies = rec.Result().Cookies()
		form = cloneValues(form)
		form.Set(websec.CSRFFieldName, m[1])
	}
	var body io.Reader
	if form != nil {
		body = strings.NewReader(form.Encode())
	}
	req := httptest.NewRequest(method, target, body)
	if form != nil {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	req.Header.Set("Sec-Fetch-Site", "same-origin")
	req.Header.Set("X-Test-Lang", lang)
	req.RemoteAddr = ip + ":1234"
	for _, c := range cookies {
		req.AddCookie(c)
	}
	rec := httptest.NewRecorder()
	e.handler.ServeHTTP(rec, req)
	return rec
}

func cloneValues(v url.Values) url.Values {
	out := url.Values{}
	for k, vs := range v {
		out[k] = append([]string(nil), vs...)
	}
	return out
}
