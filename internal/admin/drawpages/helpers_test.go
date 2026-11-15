package drawpages

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"secretsanta/internal/admin/audit"
	"secretsanta/internal/config"
	"secretsanta/internal/db"
	"secretsanta/internal/draw"
	"secretsanta/internal/groups"
	"secretsanta/internal/i18n/catalog"
	"secretsanta/internal/i18n/negotiate"
	"secretsanta/internal/participants"
	"secretsanta/internal/reveal"
	"secretsanta/internal/rules"
	"secretsanta/internal/shared/adminnav"
	"secretsanta/internal/shared/clock"
	"secretsanta/internal/shared/errs"
	"secretsanta/internal/shared/ports"
	"secretsanta/internal/websec"
	"secretsanta/web"
)

const publicURL = "http://127.0.0.1:8080"

var (
	t0  = time.Date(2026, 11, 15, 10, 0, 0, 0, time.UTC)
	ctx = context.Background()
)

type sqlDirectory struct{ db *sql.DB }

func (d sqlDirectory) DrawRole(ctx context.Context, groupID, id int64) (string, bool, error) {
	var role string
	err := d.db.QueryRowContext(ctx, `SELECT draw_role FROM participants WHERE group_id = ? AND id = ?`, groupID, id).Scan(&role)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	return role, err == nil, err
}

// fakeReveal remplace le service de révélation et garde les appels.
type fakeReveal struct {
	mu       sync.Mutex
	open     bool
	locked   bool
	at       *time.Time
	targets  int
	setCalls int
	openCall int
	setErr   error
}

func (f *fakeReveal) Facts(_ context.Context, _ participants.Actor, _ int64) (*reveal.Facts, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return &reveal.Facts{DrawDone: true, Open: f.open, DateLocked: f.locked, RevealAt: f.at, Targets: f.targets}, nil
}

func (f *fakeReveal) SetRevealDate(_ context.Context, _ participants.Actor, _ int64, at *time.Time) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.setCalls++
	if f.setErr != nil {
		return f.setErr
	}
	f.at = at
	return nil
}

func (f *fakeReveal) OpenNow(_ context.Context, _ participants.Actor, _ int64, confirmed bool) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if !confirmed {
		return false, errs.Validation("confirmation")
	}
	f.openCall++
	f.open, f.locked = true, true
	return true, nil
}

type env struct {
	t        *testing.T
	db       *sql.DB
	clock    *clock.Fake
	reg      *ports.FakeRegistrationState
	groups   *groups.Service
	parts    *participants.Service
	draws    *draw.Service
	audit    *audit.Service
	reveal   *fakeReveal
	pages    *Pages
	sessions *websec.Sessions
	cat      *catalog.Catalog
	handler  http.Handler
	names    []string
	changed  int
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
		{groups.Module, groups.Migrations},
		{participants.Module, participants.Migrations},
		{rules.Module, rules.Migrations},
		{draw.Module, draw.Migrations},
		{websec.MigrationModule, websec.Migrations},
		{audit.Module, audit.Migrations},
	}
	for _, s := range steps {
		ms, err := s.load()
		if err != nil {
			t.Fatal(err)
		}
		if err := db.Migrate(d, s.module, ms); err != nil {
			t.Fatalf("migrations %s : %v", s.module, err)
		}
	}
	clk := clock.NewFake(t0)
	gs, err := groups.NewService(d, clk, config.NewSecret("mot-de-passe-instance"))
	if err != nil {
		t.Fatal(err)
	}
	e := &env{t: t, db: d, clock: clk, groups: gs, reveal: &fakeReveal{},
		reg: &ports.FakeRegistrationState{Pending: map[int64]int{}}}
	e.parts = participants.NewService(d, clk, draw.NewState(d))
	rs := rules.NewService(d, sqlDirectory{db: d})
	e.draws = draw.NewService(d, clk, gs, e.parts, rs, e.reg, draw.Options{})
	e.audit = audit.New(d, clk)
	cat, err := catalog.Load(web.I18n, catalog.Options{DefaultLang: "fr", Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})
	if err != nil {
		t.Fatal(err)
	}
	e.cat = cat
	reg := adminnav.New()
	reg.Declare(adminnav.Entry{Key: "tirage", Label: "layout.nav.draw", Icon: "draw", Rank: adminnav.RankDraw, Destination: "/x/admin/tirage"})
	e.pages, err = NewPages(Options{Groups: gs, Draws: e.draws, Reveal: e.reveal, Translator: cat, Audit: e.audit,
		OnDrawChanged: func(context.Context, int64) error { e.changed++; return nil },
		NavFor:        func(_ *http.Request, active string) []adminnav.Item { return reg.Items(active) }})
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
	chain := websec.NewCSRF(pol, websec.CSRFOptions{}).Middleware(e.sessions.Middleware(mux))
	e.handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		lang := r.Header.Get("X-Test-Lang")
		if lang == "" {
			lang = "fr"
		}
		chain.ServeHTTP(w, r.WithContext(negotiate.WithLang(r.Context(), lang)))
	})
	return e
}

func (e *env) group(slug string) int64 {
	e.t.Helper()
	res, err := e.db.Exec(`INSERT INTO groups (slug, name, admin_secret_hash, created_at, max_gifts_per_giver)
		VALUES (?, ?, zeroblob(32), ?, 2)`, slug, "Groupe "+slug, t0.Format(time.RFC3339Nano))
	if err != nil {
		e.t.Fatal(err)
	}
	id, _ := res.LastInsertId()
	return id
}

func (e *env) add(gid int64, first string, role participants.DrawRole) int64 {
	e.t.Helper()
	name := "Zx" + first + "ovitch"
	p, err := e.parts.Create(ctx, participants.AdminActor(gid), gid, participants.Input{FirstName: first, Name: name, Role: role})
	if err != nil {
		e.t.Fatalf("ajout de %s : %v", first, err)
	}
	e.names = append(e.names, first, name)
	return p.ID
}

func (e *env) addN(gid int64, prefix string, n int, role participants.DrawRole) []int64 {
	var ids []int64
	for i := 0; i < n; i++ {
		ids = append(ids, e.add(gid, fmt.Sprintf("%s%c", prefix, 'a'+i), role))
	}
	return ids
}

func (e *env) doDraw(gid int64) {
	e.t.Helper()
	if _, err := e.draws.Draw(ctx, participants.AdminActor(gid), gid, draw.Request{Confirmed: true}); err != nil {
		e.t.Fatalf("tirage : %v", err)
	}
}

func (e *env) drawCount(gid int64) int {
	return countRows(e.t, e.db, `SELECT COUNT(*) FROM draws WHERE group_id = ?`, gid)
}

func (e *env) adminCookie(gid int64) *http.Cookie {
	e.t.Helper()
	rec := httptest.NewRecorder()
	if _, err := e.sessions.Create(ctx, rec, nil, websec.Identity{Kind: websec.KindAdmin, GroupID: strconv.FormatInt(gid, 10)}); err != nil {
		e.t.Fatal(err)
	}
	for _, c := range rec.Result().Cookies() {
		if c.Name == e.sessions.CookieName(websec.KindAdmin) {
			return c
		}
	}
	e.t.Fatal("cookie administrateur absent")
	return nil
}

var csrfRe = regexp.MustCompile(`name="csrf_token" value="([^"]+)"`)

func (e *env) get(path, lang string, ck *http.Cookie) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, path, nil)
	req.Header.Set("X-Test-Lang", lang)
	if ck != nil {
		req.AddCookie(ck)
	}
	rec := httptest.NewRecorder()
	e.handler.ServeHTTP(rec, req)
	return rec
}

// post envoie un POST avec le jeton CSRF pris sur la page tokenPage.
func (e *env) post(path, tokenPage string, ck *http.Cookie, fields url.Values) *httptest.ResponseRecorder {
	e.t.Helper()
	page := e.get(tokenPage, "fr", ck)
	m := csrfRe.FindStringSubmatch(page.Body.String())
	if m == nil {
		e.t.Fatalf("jeton CSRF introuvable sur %s : %d", tokenPage, page.Code)
	}
	form := url.Values{websec.CSRFFieldName: {m[1]}}
	for k, v := range fields {
		form[k] = v
	}
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Sec-Fetch-Site", "same-origin")
	req.Header.Set("X-Test-Lang", "fr")
	req.AddCookie(ck)
	for _, c := range page.Result().Cookies() {
		if c.Name != ck.Name {
			req.AddCookie(c)
		}
	}
	rec := httptest.NewRecorder()
	e.handler.ServeHTTP(rec, req)
	return rec
}

// noNames vérifie qu'aucun nom de la fixture ne figure dans le corps.
func (e *env) noNames(t *testing.T, where, body string) {
	t.Helper()
	for _, n := range e.names {
		if strings.Contains(body, n) {
			t.Errorf("%s : le nom %q apparaît", where, n)
		}
	}
}

func countRows(t *testing.T, d *sql.DB, query string, args ...any) int {
	t.Helper()
	var n int
	if err := d.QueryRow(query, args...).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}
