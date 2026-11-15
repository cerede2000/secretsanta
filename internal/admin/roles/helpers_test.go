package roles

import (
	"context"
	"database/sql"
	"errors"
	"html/template"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
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
	"secretsanta/internal/rulechange"
	"secretsanta/internal/rules"
	"secretsanta/internal/shared/adminnav"
	"secretsanta/internal/shared/clock"
	"secretsanta/internal/shared/ports"
	"secretsanta/internal/websec"
	"secretsanta/web"
)

const publicURL = "http://127.0.0.1:8080"

var t0 = time.Date(2026, 11, 15, 10, 0, 0, 0, time.UTC)

type sqlDirectory struct{ db *sql.DB }

func (d sqlDirectory) DrawRole(ctx context.Context, groupID, id int64) (string, bool, error) {
	var role string
	err := d.db.QueryRowContext(ctx, `SELECT draw_role FROM participants WHERE group_id = ? AND id = ?`, groupID, id).Scan(&role)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	return role, err == nil, err
}

type journalEntry struct {
	action string
	counts map[string]int
}

type env struct {
	db       *sql.DB
	pages    *Pages
	handler  http.Handler
	sessions *websec.Sessions
	state    *ports.FakeDrawState
	repair   *ports.FakeRuleChangeRepair
	parts    *participants.Service
	cat      *catalog.Catalog
	journal  []journalEntry
	groupA   int64
	groupB   int64
}

func newEnv(t *testing.T) *env {
	t.Helper()
	d, err := db.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = d.Close() })
	steps := []struct {
		module string
		load   func() ([]db.Migration, error)
	}{
		{websec.MigrationModule, websec.Migrations},
		{groups.Module, groups.Migrations},
		{participants.Module, participants.Migrations},
		{rules.Module, rules.Migrations},
		{draw.Module, draw.Migrations},
		{rulechange.Module, rulechange.Migrations},
	}
	for _, s := range steps {
		ms, err := s.load()
		if err != nil {
			t.Fatal(err)
		}
		if err := db.Migrate(d, s.module, ms); err != nil {
			t.Fatal(err)
		}
	}
	clk := clock.NewFake(t0)
	gs, err := groups.NewService(d, clk, config.NewSecret("mot-de-passe-instance"))
	if err != nil {
		t.Fatal(err)
	}
	state := &ports.FakeDrawState{Done: map[int64]bool{}}
	parts := participants.NewService(d, clk, state)
	rs := rules.NewService(d, sqlDirectory{db: d})
	reg := &ports.FakeRegistrationState{Pending: map[int64]int{}}
	ds := draw.NewService(d, clk, gs, parts, rs, reg, draw.Options{})
	repair := &ports.FakeRuleChangeRepair{}
	rc := rulechange.NewService(d, clk, state, repair)
	cat, err := catalog.Load(web.I18n, catalog.Options{DefaultLang: "fr", Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})
	if err != nil {
		t.Fatal(err)
	}
	e := &env{db: d, state: state, repair: repair, parts: parts, cat: cat}
	e.pages, err = New(Options{
		Groups: gs, People: parts, Roles: rc, Check: ds, State: state, Translator: cat,
		Nav: func(_ *http.Request, active string) []adminnav.Item {
			r := adminnav.New()
			r.Declare(adminnav.Entry{Key: "tableau", Label: "layout.nav.label", Icon: "dashboard", Rank: adminnav.RankDashboard, Destination: "/noel/admin"})
			r.Declare(adminnav.Entry{Key: "tirage", Label: "layout.nav.label", Icon: "draw", Rank: adminnav.RankDraw, Destination: "/noel/admin/tirage"})
			return r.Items(active)
		},
		Journal: func(_ context.Context, _ int64, action string, counts map[string]int) {
			e.journal = append(e.journal, journalEntry{action, counts})
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	pol, err := websec.NewPolicy(publicURL)
	if err != nil {
		t.Fatal(err)
	}
	e.sessions = websec.NewSessions(d, clk, pol, websec.SessionOptions{})
	mux := http.NewServeMux()
	e.pages.Register(mux)
	csrf := websec.NewCSRF(pol, websec.CSRFOptions{}).Middleware(e.sessions.Middleware(mux))
	e.handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		lang := r.Header.Get("X-Test-Lang")
		if lang == "" {
			lang = "fr"
		}
		csrf.ServeHTTP(w, r.WithContext(negotiate.WithLang(r.Context(), lang)))
	})
	e.groupA = e.group(t, "noel")
	e.groupB = e.group(t, "paques")
	return e
}

func (e *env) group(t *testing.T, slug string) int64 {
	t.Helper()
	res, err := e.db.Exec(`INSERT INTO groups (slug, name, admin_secret_hash, created_at, max_gifts_per_giver)
		VALUES (?, 'Famille', zeroblob(32), ?, 2)`, slug, t0.Format(time.RFC3339Nano))
	if err != nil {
		t.Fatal(err)
	}
	id, _ := res.LastInsertId()
	return id
}

// person ajoute un participant au nom reconnaissable.
func (e *env) person(t *testing.T, gid int64, first string, role participants.DrawRole) int64 {
	t.Helper()
	p, err := e.parts.Create(context.Background(), participants.AdminActor(gid), gid,
		participants.Input{FirstName: first, Name: "Zx" + first + "ovitch", Role: role})
	if err != nil {
		t.Fatal(err)
	}
	return p.ID
}

func (e *env) roleOf(t *testing.T, id int64) string {
	t.Helper()
	var r string
	if err := e.db.QueryRow(`SELECT draw_role FROM participants WHERE id = ?`, id).Scan(&r); err != nil {
		t.Fatal(err)
	}
	return r
}

// adminCookies ouvre une session administrateur du groupe.
func (e *env) adminCookies(t *testing.T, gid int64) []*http.Cookie {
	t.Helper()
	rec := httptest.NewRecorder()
	if _, err := e.sessions.Create(context.Background(), rec, nil, websec.Identity{Kind: websec.KindAdmin, GroupID: strconv.FormatInt(gid, 10)}); err != nil {
		t.Fatal(err)
	}
	return rec.Result().Cookies()
}

var csrfRe = regexp.MustCompile(`name="csrf_token" value="([^"]+)"`)

func (e *env) get(t *testing.T, target, lang string, cookies []*http.Cookie) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, target, nil)
	req.Header.Set("X-Test-Lang", lang)
	for _, c := range cookies {
		req.AddCookie(c)
	}
	rec := httptest.NewRecorder()
	e.handler.ServeHTTP(rec, req)
	return rec
}

// post envoie un formulaire avec le jeton CSRF pris sur la page du groupe.
func (e *env) post(t *testing.T, slug string, form url.Values, lang string, cookies []*http.Cookie) *httptest.ResponseRecorder {
	t.Helper()
	return e.postAs(t, slug, form, lang, cookies, cookies)
}

// postAs prend le jeton CSRF avec pageCookies et envoie avec sendCookies.
func (e *env) postAs(t *testing.T, slug string, form url.Values, lang string, pageCookies, sendCookies []*http.Cookie) *httptest.ResponseRecorder {
	t.Helper()
	page := e.get(t, "/"+slug+"/admin/roles", lang, pageCookies)
	m := csrfRe.FindStringSubmatch(page.Body.String())
	if m == nil {
		t.Fatalf("jeton CSRF introuvable : %d", page.Code)
	}
	all := append(append([]*http.Cookie{}, sendCookies...), page.Result().Cookies()...)
	f := url.Values{}
	for k, v := range form {
		f[k] = v
	}
	f.Set(websec.CSRFFieldName, m[1])
	req := httptest.NewRequest(http.MethodPost, "/"+slug+"/admin/roles", strings.NewReader(f.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Sec-Fetch-Site", "same-origin")
	req.Header.Set("X-Test-Lang", lang)
	for _, c := range all {
		req.AddCookie(c)
	}
	rec := httptest.NewRecorder()
	e.handler.ServeHTTP(rec, req)
	return rec
}

func ids(v ...int64) []string {
	out := make([]string, len(v))
	for i, x := range v {
		out[i] = strconv.FormatInt(x, 10)
	}
	return out
}

func httpGet(target string) *http.Request { return httptest.NewRequest(http.MethodGet, target, nil) }

func testFuncs(e *env, r *http.Request) template.FuncMap {
	f := template.FuncMap{}
	for k, v := range websec.TemplateFuncs(r) {
		f[k] = v
	}
	for k, v := range i18nweb.Funcs(e.cat, r) {
		f[k] = v
	}
	return f
}
