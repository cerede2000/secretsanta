package grouppages

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"html/template"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"
	"time"

	"secretsanta/internal/config"
	"secretsanta/internal/db"
	"secretsanta/internal/groups"
	"secretsanta/internal/i18n/catalog"
	"secretsanta/internal/i18n/negotiate"
	i18nweb "secretsanta/internal/i18n/web"
	"secretsanta/internal/shared/clock"
	"secretsanta/internal/websec"
	"secretsanta/web"
)

const (
	testInstancePassword = "mot-de-passe-instance-correct"
	testOrigin           = "http://127.0.0.1:8080"
)

// testEnv assemble les vraies pièces : service des groupes sur une base
// temporaire, catalogue réel, gabarits réels avec le layout réel, chaîne CSRF.
type testEnv struct {
	db      *sql.DB
	svc     *groups.Service
	cat     *catalog.Catalog
	pages   *Pages
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

// realRenderer analyse layout, i18n et groups comme le fera l'orchestrateur,
// puis rend sur un clone avec t, tn et csrfField liés à la requête.
func realRenderer(t *testing.T, cat *catalog.Catalog) Renderer {
	t.Helper()
	tm := template.New("root").Funcs(websec.TemplateFuncsPlaceholder()).Funcs(i18nweb.FuncsPlaceholder())
	tm, err := tm.ParseFS(web.Templates, "templates/layout/*.html", "templates/i18n/*.html", "templates/groups/*.html")
	if err != nil {
		t.Fatal(err)
	}
	return func(w http.ResponseWriter, r *http.Request, status int, name string, data any) {
		c, err := tm.Clone()
		if err != nil {
			t.Errorf("clone : %v", err)
			return
		}
		c = c.Funcs(websec.TemplateFuncs(r)).Funcs(i18nweb.Funcs(cat, r))
		var buf bytes.Buffer
		if err := c.ExecuteTemplate(&buf, name, data); err != nil {
			t.Errorf("rendu de %s : %v", name, err)
			http.Error(w, "erreur", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(status)
		_, _ = w.Write(buf.Bytes())
	}
}

func newEnv(t *testing.T) *testEnv {
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
	svc, err := groups.NewService(d, clock.NewFake(time.Date(2026, 11, 15, 10, 0, 0, 0, time.UTC)), config.NewSecret(testInstancePassword))
	if err != nil {
		t.Fatal(err)
	}
	cat := realCatalog(t)
	p, err := New(Deps{Groups: svc, Render: realRenderer(t, cat), PublicURL: testOrigin})
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	p.Register(mux)
	pol, err := websec.NewPolicy(testOrigin)
	if err != nil {
		t.Fatal(err)
	}
	return &testEnv{db: d, svc: svc, cat: cat, pages: p, handler: websec.NewCSRF(pol, websec.CSRFOptions{}).Middleware(mux)}
}

// do envoie une requête ; lang vide : français par défaut (en-tête absent).
func (e *testEnv) do(method, target, lang string, form url.Values, cookies []*http.Cookie) *httptest.ResponseRecorder {
	var body io.Reader
	if form != nil {
		body = strings.NewReader(form.Encode())
	}
	r := httptest.NewRequest(method, target, body)
	if form != nil {
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	r.Header.Set("Sec-Fetch-Site", "same-origin")
	if lang != "" {
		r = r.WithContext(withLang(r.Context(), lang))
	}
	for _, c := range cookies {
		r.AddCookie(c)
	}
	w := httptest.NewRecorder()
	e.handler.ServeHTTP(w, r)
	return w
}

var csrfRe = regexp.MustCompile(`name="csrf_token" value="([^"]+)"`)

// postCreate ouvre le formulaire (cookie et jeton CSRF) puis le soumet.
func (e *testEnv) postCreate(t *testing.T, lang string, form url.Values) *httptest.ResponseRecorder {
	t.Helper()
	get := e.do("GET", "/creer", lang, nil, nil)
	m := csrfRe.FindStringSubmatch(get.Body.String())
	if m == nil {
		t.Fatalf("jeton CSRF absent du formulaire : %s", get.Body.String())
	}
	form.Set("csrf_token", html2text(m[1]))
	return e.do("POST", "/creer", lang, form, get.Result().Cookies())
}

func html2text(s string) string { return strings.NewReplacer("&#43;", "+", "&amp;", "&").Replace(s) }

func validForm(slug string) url.Values {
	return url.Values{
		"password": {testInstancePassword},
		"name":     {"Family Martin"},
		"slug":     {slug},
		"budget":   {""},
	}
}

// catalogFile lit un catalogue du module groups.
func catalogFile(t *testing.T, lang string) map[string]string {
	t.Helper()
	raw, err := web.I18n.ReadFile("static/i18n/groups/" + lang + ".json")
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]string{}
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatal(err)
	}
	return out
}

var _ = context.Background

func withLang(ctx context.Context, lang string) context.Context {
	return negotiate.WithLang(ctx, lang)
}
