package menu

import (
	"bytes"
	"context"
	"encoding/json"
	"html/template"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"secretsanta/internal/db"
	"secretsanta/internal/groups"
	"secretsanta/internal/i18n/catalog"
	"secretsanta/internal/i18n/negotiate"
	i18nweb "secretsanta/internal/i18n/web"
	"secretsanta/internal/shared/adminnav"
	"secretsanta/internal/shared/clock"
	"secretsanta/internal/websec"
	"secretsanta/web"
)

const testOrigin = "http://127.0.0.1:8080"

type fakeGroups struct{ byslug map[string]*groups.Group }

func (f fakeGroups) BySlug(_ context.Context, slug string) (*groups.Group, error) {
	if g, ok := f.byslug[slug]; ok {
		return g, nil
	}
	return nil, http.ErrMissingFile
}

type env struct {
	pages    *Pages
	handler  http.Handler
	sessions *websec.Sessions
	nav      *adminnav.Registry
	cookies  map[string][]*http.Cookie // slug -> cookies administrateur
	cat      *catalog.Catalog
}

func realCatalog(t *testing.T) *catalog.Catalog {
	t.Helper()
	cat, err := catalog.Load(web.I18n, catalog.Options{DefaultLang: "fr", Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})
	if err != nil {
		t.Fatal(err)
	}
	return cat
}

func realRenderer(t *testing.T, cat *catalog.Catalog) Renderer {
	t.Helper()
	tm := template.New("root").Funcs(websec.TemplateFuncsPlaceholder()).Funcs(i18nweb.FuncsPlaceholder())
	tm, err := tm.ParseFS(web.Templates, "templates/layout/*.html", "templates/i18n/*.html", "templates/admin/menu/*.html")
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

func newEnv(t *testing.T) *env {
	t.Helper()
	d, err := db.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = d.Close() })
	ms, err := websec.Migrations()
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Migrate(d, websec.MigrationModule, ms); err != nil {
		t.Fatal(err)
	}
	pol, err := websec.NewPolicy(testOrigin)
	if err != nil {
		t.Fatal(err)
	}
	sess := websec.NewSessions(d, clock.NewFake(time.Date(2026, 11, 15, 10, 0, 0, 0, time.UTC)), pol, websec.SessionOptions{})
	reg := adminnav.New()
	DeclareNav(reg)
	DeclareParticipants(reg)
	g1 := &groups.Group{ID: 1, Slug: "famille", Name: "Famille"}
	g2 := &groups.Group{ID: 2, Slug: "amis", Name: "Amis"}
	cat := realCatalog(t)
	p, err := New(Deps{Groups: fakeGroups{map[string]*groups.Group{"famille": g1, "amis": g2}}, Sessions: sess, Render: realRenderer(t, cat), Nav: reg})
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	p.Register(mux)
	e := &env{pages: p, sessions: sess, nav: reg, cat: cat, cookies: map[string][]*http.Cookie{}}
	e.handler = websec.NewCSRF(pol, websec.CSRFOptions{}).Middleware(sess.Middleware(mux))
	for slug, g := range map[string]*groups.Group{"famille": g1, "amis": g2} {
		w := httptest.NewRecorder()
		r := httptest.NewRequest("GET", "/", nil)
		if _, err := sess.Create(context.Background(), w, r, websec.Identity{Kind: websec.KindAdmin, GroupID: strconv.FormatInt(g.ID, 10)}); err != nil {
			t.Fatal(err)
		}
		e.cookies[slug] = w.Result().Cookies()
	}
	return e
}

func (e *env) do(method, target, lang string, form url.Values, cookies []*http.Cookie) *httptest.ResponseRecorder {
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
		r = r.WithContext(negotiate.WithLang(r.Context(), lang))
	}
	for _, c := range cookies {
		r.AddCookie(c)
	}
	w := httptest.NewRecorder()
	e.handler.ServeHTTP(w, r)
	return w
}

func catalogFile(t *testing.T, lang string) map[string]string {
	t.Helper()
	raw, err := web.I18n.ReadFile("static/i18n/admin/menu/" + lang + ".json")
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]string{}
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatal(err)
	}
	return out
}
