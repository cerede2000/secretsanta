package targetpage

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"html"
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

	"secretsanta/internal/config"
	"secretsanta/internal/db"
	"secretsanta/internal/draw"
	"secretsanta/internal/groups"
	"secretsanta/internal/i18n/catalog"
	"secretsanta/internal/i18n/negotiate"
	i18nweb "secretsanta/internal/i18n/web"
	"secretsanta/internal/participants"
	"secretsanta/internal/reveal"
	"secretsanta/internal/rules"
	"secretsanta/internal/shared/clock"
	"secretsanta/internal/shared/ports"
	"secretsanta/internal/websec"
	"secretsanta/web"
)

const testOrigin = "http://127.0.0.1:8080"

var t0 = time.Date(2026, 11, 15, 10, 0, 0, 0, time.UTC)

var ctx = context.Background()

// sqlDirectory adapte la table participants au port rules.Directory.
type sqlDirectory struct{ db *sql.DB }

func (d sqlDirectory) DrawRole(ctx context.Context, groupID, id int64) (string, bool, error) {
	var role string
	err := d.db.QueryRowContext(ctx, `SELECT draw_role FROM participants WHERE group_id = ? AND id = ?`, groupID, id).Scan(&role)
	if err == sql.ErrNoRows {
		return "", false, nil
	}
	return role, err == nil, err
}

// env assemble les vraies pièces : services, catalogue, gabarits, sessions,
// chaîne CSRF.
type env struct {
	db       *sql.DB
	clock    *clock.Fake
	groups   *groups.Service
	parts    *participants.Service
	draw     *draw.Service
	store    *Store
	sessions *websec.Sessions
	pages    *Pages
	handler  http.Handler
	jar      map[string]*http.Cookie
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
	tm, err := tm.ParseFS(web.Templates, "templates/layout/*.html", "templates/i18n/*.html", "templates/draw/*.html")
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
	for _, s := range []struct {
		module string
		load   func() ([]db.Migration, error)
	}{
		{websec.MigrationModule, websec.Migrations},
		{groups.Module, groups.Migrations},
		{participants.Module, participants.Migrations},
		{rules.Module, rules.Migrations},
		{draw.Module, draw.Migrations},
		{reveal.Module, reveal.Migrations},
		{Module, Migrations},
	} {
		ms, err := s.load()
		if err != nil || len(ms) == 0 {
			t.Fatalf("migrations %s : %v", s.module, err)
		}
		if err := db.Migrate(d, s.module, ms); err != nil {
			t.Fatal(err)
		}
	}
	c := clock.NewFake(t0)
	g, err := groups.NewService(d, c, config.NewSecret("mot-de-passe-instance"))
	if err != nil {
		t.Fatal(err)
	}
	e := &env{db: d, clock: c, groups: g, jar: map[string]*http.Cookie{}, store: NewStore(d)}
	e.parts = participants.NewService(d, c, draw.NewState(d))
	r := rules.NewService(d, sqlDirectory{db: d})
	e.draw = draw.NewService(d, c, g, e.parts, r, &ports.FakeRegistrationState{Pending: map[int64]int{}},
		draw.Options{Gate: reveal.NewGate(d, c)})
	pol, err := websec.NewPolicy(testOrigin)
	if err != nil {
		t.Fatal(err)
	}
	e.sessions = websec.NewSessions(d, c, pol, websec.SessionOptions{})
	cat := realCatalog(t)
	e.pages, err = New(Deps{Groups: g, Participants: e.parts, Draw: e.draw, Budget: e.store, Render: realRenderer(t, cat), Clock: c})
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	mux.Handle("GET /{slug}/participant", e.pages.Home())
	mux.Handle("GET /{slug}/ma-cible", e.pages.Home())
	mux.HandleFunc("GET /{slug}/_token", func(w http.ResponseWriter, r *http.Request) {
		tok, err := websec.Token(r)
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		_, _ = w.Write([]byte(tok))
	})
	mux.Handle("/{slug}/preference", e.pages.Preference())
	mux.Handle("/{slug}/budget-vu", e.pages.BudgetSeen())
	e.handler = websec.NewCSRF(pol, websec.CSRFOptions{}).Middleware(e.sessions.Middleware(mux))
	return e
}

// group crée un groupe ; revealAt nil : révélation immédiate après tirage.
func (e *env) group(t *testing.T, slug string, revealAt *time.Time) int64 {
	t.Helper()
	var reveal any
	if revealAt != nil {
		reveal = revealAt.UTC().Format(time.RFC3339)
	}
	res, err := e.db.Exec(`INSERT INTO groups (slug, name, admin_secret_hash, created_at, max_gifts_per_giver, reveal_at)
		VALUES (?, 'Famille', zeroblob(32), ?, 2, ?)`, slug, t0.Format(time.RFC3339Nano), reveal)
	if err != nil {
		t.Fatal(err)
	}
	id, _ := res.LastInsertId()
	return id
}

func (e *env) add(t *testing.T, gid int64, first string, role participants.DrawRole) int64 {
	t.Helper()
	p, err := e.parts.Create(ctx, participants.AdminActor(gid), gid,
		participants.Input{FirstName: first, Name: "Zq" + first + "enko", Role: role, GiftHint: "Indice de " + first})
	if err != nil {
		t.Fatalf("ajout de %s : %v", first, err)
	}
	return p.ID
}

func (e *env) mustDraw(t *testing.T, gid int64) {
	t.Helper()
	if _, err := e.draw.Draw(ctx, participants.AdminActor(gid), gid, draw.Request{Confirmed: true}); err != nil {
		t.Fatalf("tirage refusé : %v", err)
	}
}

// setBudget fixe le budget et sa version comme le fera F2e.
func (e *env) setBudget(t *testing.T, gid int64, cents *int64, version int) {
	t.Helper()
	if _, err := e.db.Exec(`UPDATE groups SET budget_cents = ?, budget_version = ? WHERE id = ?`, cents, version, gid); err != nil {
		t.Fatal(err)
	}
}

func (e *env) setEventDate(t *testing.T, gid int64, iso string) {
	t.Helper()
	if _, err := e.db.Exec(`UPDATE groups SET event_date = ? WHERE id = ?`, iso, gid); err != nil {
		t.Fatal(err)
	}
}

// login ouvre une session participant ; les cookies vont dans le jar.
func (e *env) login(t *testing.T, gid, pid int64) {
	t.Helper()
	rec := httptest.NewRecorder()
	if _, err := e.sessions.Create(ctx, rec, httptest.NewRequest("POST", "/", nil), websec.Identity{
		Kind: websec.KindParticipant, GroupID: strconv.FormatInt(gid, 10), Subject: strconv.FormatInt(pid, 10)}); err != nil {
		t.Fatal(err)
	}
	e.keep(rec)
}

func (e *env) keep(rec *httptest.ResponseRecorder) {
	for _, c := range rec.Result().Cookies() {
		e.jar[c.Name] = c
	}
}

// do envoie une requête avec le jar ; lang vide : français.
func (e *env) do(method, target, lang string, form url.Values) *httptest.ResponseRecorder {
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
	for _, c := range e.jar {
		r.AddCookie(&http.Cookie{Name: c.Name, Value: c.Value})
	}
	w := httptest.NewRecorder()
	e.handler.ServeHTTP(w, r)
	e.keep(w)
	return w
}

// post charge d'abord la page (cookie et jeton CSRF), puis envoie le formulaire.
func (e *env) post(t *testing.T, slug, path, lang string, form url.Values) *httptest.ResponseRecorder {
	t.Helper()
	token := e.token(t, slug, lang)
	if form == nil {
		form = url.Values{}
	}
	form.Set("csrf_token", token)
	return e.do("POST", "/"+slug+path, lang, form)
}

// token lit un jeton CSRF (et pose le cookie CSRF) sur une route de test.
func (e *env) token(t *testing.T, slug, lang string) string {
	t.Helper()
	w := e.do("GET", "/"+slug+"/_token", lang, nil)
	if w.Code != 200 || w.Body.Len() == 0 {
		t.Fatalf("jeton CSRF absent (%d)", w.Code)
	}
	return w.Body.String()
}

func catalogFile(t *testing.T, lang string) map[string]string {
	t.Helper()
	raw, err := web.I18n.ReadFile("static/i18n/draw/" + lang + ".json")
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]string{}
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatal(err)
	}
	return out
}

func ptr(v int64) *int64 { return &v }

func at(d time.Duration) *time.Time { v := t0.Add(d); return &v }

func mustContain(t *testing.T, body, sub string) {
	t.Helper()
	if !strings.Contains(html.UnescapeString(body), sub) {
		t.Errorf("la page devrait contenir %q :\n%s", sub, body)
	}
}

func mustNotContain(t *testing.T, body, sub string) {
	t.Helper()
	if strings.Contains(html.UnescapeString(body), sub) {
		t.Errorf("la page ne devrait pas contenir %q", sub)
	}
}

var _ = fmt.Sprint
