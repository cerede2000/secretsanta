package settingspages

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

	"secretsanta/internal/config"
	"secretsanta/internal/db"
	"secretsanta/internal/draw"
	"secretsanta/internal/groups"
	"secretsanta/internal/groupsettings"
	"secretsanta/internal/i18n/catalog"
	"secretsanta/internal/i18n/negotiate"
	"secretsanta/internal/mail/queue"
	"secretsanta/internal/participants"
	"secretsanta/internal/reminderrules"
	"secretsanta/internal/reminders"
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

type fakeReveal struct {
	mu       sync.Mutex
	locked   bool
	at       *time.Time
	setCalls int
}

func (f *fakeReveal) Facts(context.Context, participants.Actor, int64) (*reveal.Facts, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return &reveal.Facts{DrawDone: true, DateLocked: f.locked, RevealAt: f.at}, nil
}

func (f *fakeReveal) SetRevealDate(_ context.Context, _ participants.Actor, _ int64, at *time.Time) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.setCalls++
	f.at = at
	return nil
}

type fakeReminders struct {
	calls int
	kind  reminders.Kind
	ids   []int64
}

func (f *fakeReminders) Remind(_ context.Context, _ int64, kind reminders.Kind, ids []int64) (reminders.ManualResult, error) {
	f.calls++
	f.kind, f.ids = kind, ids
	var res reminders.ManualResult
	for _, id := range ids {
		res.Items = append(res.Items, reminders.ManualItem{ParticipantID: id, Name: "X", Outcome: reminders.OutcomeQueued})
	}
	return res, nil
}

type fakeMail struct {
	calls int
	item  queue.Item
}

func (f *fakeMail) SendTest(_ context.Context, it queue.Item) (queue.Status, error) {
	f.calls++
	f.item = it
	return queue.Status{State: queue.StateSent, SentAt: t0}, nil
}

type env struct {
	t        *testing.T
	db       *sql.DB
	clock    *clock.Fake
	groups   *groups.Service
	parts    *participants.Service
	draws    *draw.Service
	settings *groupsettings.Service
	bus      *ports.MemoryEventBus
	reveal   *fakeReveal
	rem      *fakeReminders
	mail     *fakeMail
	pages    *Pages
	sessions *websec.Sessions
	cat      *catalog.Catalog
	handler  http.Handler
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
		{groupsettings.Module, groupsettings.Migrations},
		{reminderrules.Module, reminderrules.Migrations},
		{websec.MigrationModule, websec.Migrations},
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
	cat, err := catalog.Load(web.I18n, catalog.Options{DefaultLang: "fr", Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})
	if err != nil {
		t.Fatal(err)
	}
	e := &env{t: t, db: d, clock: clk, groups: gs, reveal: &fakeReveal{}, rem: &fakeReminders{}, mail: &fakeMail{}, cat: cat,
		bus: &ports.MemoryEventBus{}}
	e.parts = participants.NewService(d, clk, draw.NewState(d))
	rs := rules.NewService(d, sqlDirectory{db: d})
	e.draws = draw.NewService(d, clk, gs, e.parts, rs, &ports.FakeRegistrationState{Pending: map[int64]int{}}, draw.Options{})
	e.settings = groupsettings.NewService(d, clk, e.bus, draw.NewState(d), cat)
	nav := adminnav.New()
	nav.Declare(adminnav.Entry{Key: "plus", Label: "layout.nav.more", Icon: "more", Rank: adminnav.RankMore, Destination: "/x/admin/plus"})
	e.pages, err = NewPages(Options{Groups: gs, Settings: e.settings, Reveal: e.reveal,
		Rhythm: reminderrules.NewService(d, clk, time.UTC), Reminders: e.rem, People: e.parts, Mail: e.mail, Translator: cat,
		SMTP:     config.SMTP{Host: "smtp.interne.lan", Port: 587, From: "santa@exemple.fr"},
		Delivery: config.Mail{TLSMode: config.TLSStartTLS, EHLOHostname: "santa.exemple.fr", MaxPerMinute: 20},
		NavFor:   func(_ *http.Request, active string) []adminnav.Item { return nav.Items(active) }})
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

func (e *env) addN(gid int64, prefix string, n int) []int64 {
	var ids []int64
	for i := 0; i < n; i++ {
		first := fmt.Sprintf("%s%c", prefix, 'a'+i)
		p, err := e.parts.Create(ctx, participants.AdminActor(gid), gid, participants.Input{FirstName: first, Name: "Zx" + first + "ovitch", Role: participants.RoleNormal})
		if err != nil {
			e.t.Fatal(err)
		}
		ids = append(ids, p.ID)
	}
	return ids
}

func (e *env) doDraw(gid int64) {
	e.t.Helper()
	if _, err := e.draws.Draw(ctx, participants.AdminActor(gid), gid, draw.Request{Confirmed: true}); err != nil {
		e.t.Fatalf("tirage : %v", err)
	}
}

// pairs renvoie une empreinte des paires, pour prouver qu'elles ne bougent pas.
func (e *env) pairs(gid int64) string {
	e.t.Helper()
	rows, err := e.db.Query(`SELECT p.giver_id, p.receiver_id FROM draw_pairs p JOIN draws d ON d.id = p.draw_id
		WHERE d.group_id = ? ORDER BY p.giver_id, p.receiver_id`, gid)
	if err != nil {
		e.t.Fatal(err)
	}
	defer rows.Close()
	var b strings.Builder
	for rows.Next() {
		var g, r int64
		_ = rows.Scan(&g, &r)
		fmt.Fprintf(&b, "%d>%d;", g, r)
	}
	return b.String()
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

func (e *env) post(path, tokenPage string, ck *http.Cookie, fields url.Values) *httptest.ResponseRecorder {
	e.t.Helper()
	return e.postLang(path, tokenPage, ck, fields, "fr")
}

func itoa(n int64) string { return strconv.FormatInt(n, 10) }

func (e *env) postLang(path, tokenPage string, ck *http.Cookie, fields url.Values, lang string) *httptest.ResponseRecorder {
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
	req.Header.Set("X-Test-Lang", lang)
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

// groupForm renvoie une saisie complète valide de la section « Le groupe ».
func groupFields(name string) url.Values {
	return url.Values{"name": {name}, "budget": {""}, "event_date": {""}, "reveal": {""}, "instructions": {""}, "max_gifts": {"2"}, "lang": {""}}
}

type groupRow struct {
	name, instr string
	budget      sql.NullInt64
	version     int
	date        sql.NullString
	max         int
}

func (e *env) row(gid int64) groupRow {
	e.t.Helper()
	var r groupRow
	err := e.db.QueryRow(`SELECT name, gift_instructions, budget_cents, budget_version, event_date, max_gifts_per_giver FROM groups WHERE id = ?`, gid).
		Scan(&r.name, &r.instr, &r.budget, &r.version, &r.date, &r.max)
	if err != nil {
		e.t.Fatal(err)
	}
	return r
}

func countRows(t *testing.T, d *sql.DB, query string, args ...any) int {
	t.Helper()
	var n int
	if err := d.QueryRow(query, args...).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

var _ = errs.KindValidation
