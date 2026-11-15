package people

import (
	"context"
	"database/sql"
	"errors"
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

	accessmig "secretsanta/internal/access/migrations"
	"secretsanta/internal/access/sqlstore"
	"secretsanta/internal/access/token"
	"secretsanta/internal/admin/audit"
	adminmig "secretsanta/internal/admin/migrations"
	"secretsanta/internal/admin/roles"
	"secretsanta/internal/config"
	"secretsanta/internal/db"
	"secretsanta/internal/draw"
	"secretsanta/internal/groups"
	"secretsanta/internal/i18n/catalog"
	"secretsanta/internal/i18n/negotiate"
	mailmig "secretsanta/internal/mail/migrations"
	"secretsanta/internal/mail/queue"
	"secretsanta/internal/mailunsub"
	unsubmig "secretsanta/internal/mailunsub/migrations"
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

type fakeMail struct{ items []queue.Item }

func (f *fakeMail) EnqueueMail(_ context.Context, it queue.Item) (int64, error) {
	f.items = append(f.items, it)
	return int64(len(f.items)), nil
}

type env struct {
	db       *sql.DB
	pages    *Pages
	handler  http.Handler
	sessions *websec.Sessions
	state    *ports.FakeDrawState
	repair   *ports.FakeRuleChangeRepair
	parts    *participants.Service
	tokens   *token.Service
	mail     *fakeMail
	audit    *audit.Service
	unsub    *mailunsub.Service
	cat      *catalog.Catalog
	clk      *clock.Fake
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
		{accessmig.Module, accessmig.Migrations},
		{mailmig.Module, mailmig.Migrations},
		{unsubmig.Module, unsubmig.Migrations},
		{adminmig.Module, adminmig.Migrations},
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
	tk, err := token.NewService(sqlstore.New(d), token.LoginCodeLength, []byte("cle-de-signature-de-test-0123456789"))
	if err != nil {
		t.Fatal(err)
	}
	un, err := mailunsub.NewService(mailunsub.Options{DB: d, Clock: clk, SigningKey: []byte("cle-de-signature-de-test-0123456789"), PublicURL: "https://noel.example.org"})
	if err != nil {
		t.Fatal(err)
	}
	cat, err := catalog.Load(web.I18n, catalog.Options{DefaultLang: "fr", Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})
	if err != nil {
		t.Fatal(err)
	}
	e := &env{db: d, state: state, repair: repair, parts: parts, tokens: tk, mail: &fakeMail{}, audit: audit.New(d, clk), unsub: un, cat: cat, clk: clk}
	nav := func(_ *http.Request, active string) []adminnav.Item {
		r := adminnav.New()
		r.Declare(adminnav.Entry{Key: "participants", Label: "layout.nav.label", Icon: "participants", Rank: adminnav.RankParticipants, Destination: "/noel/admin/participants"})
		return r.Items(active)
	}
	e.pages, err = New(Options{
		DB: d, Clock: clk, Groups: gs, People: parts, Credentials: tk, Mail: e.mail, Unsub: un, Rules: rs, Changes: rc,
		Audit: e.audit, State: state, Catalog: cat, Nav: nav,
		LinkURL: func(slug, tok string) string { return "https://noel.example.org/" + slug + "/acces/" + tok },
	})
	if err != nil {
		t.Fatal(err)
	}
	rp, err := roles.New(roles.Options{Groups: gs, People: parts, Roles: rc, Check: ds, State: state, Translator: cat, Nav: nav})
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
	rp.Register(mux)
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

// person ajoute un participant au nom reconnaissable ; email vide = sans mail.
func (e *env) person(t *testing.T, gid int64, first, email string, role participants.DrawRole) int64 {
	t.Helper()
	p, err := e.parts.Create(context.Background(), participants.AdminActor(gid), gid,
		participants.Input{FirstName: first, Name: "Zx" + first + "ovitch", Email: email, Role: role})
	if err != nil {
		t.Fatal(err)
	}
	return p.ID
}

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

// post envoie un formulaire ; le jeton CSRF est pris sur la page du groupe.
func (e *env) post(t *testing.T, target string, form url.Values, lang string, cookies []*http.Cookie) *httptest.ResponseRecorder {
	t.Helper()
	slug := strings.Split(strings.TrimPrefix(target, "/"), "/")[0]
	page := e.get(t, "/"+slug+"/admin/participants", lang, cookies)
	m := csrfRe.FindStringSubmatch(page.Body.String())
	if m == nil {
		t.Fatalf("jeton CSRF introuvable : %d", page.Code)
	}
	all := append(append([]*http.Cookie{}, cookies...), page.Result().Cookies()...)
	f := url.Values{}
	for k, v := range form {
		f[k] = v
	}
	f.Set(websec.CSRFFieldName, m[1])
	req := httptest.NewRequest(http.MethodPost, target, strings.NewReader(f.Encode()))
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

func sid(id int64) string { return strconv.FormatInt(id, 10) }

func (e *env) auditActions(t *testing.T, gid int64) []string {
	t.Helper()
	rows, err := e.db.Query(`SELECT action FROM admin_audit WHERE group_id = ? ORDER BY id`, gid)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var a string
		if err := rows.Scan(&a); err != nil {
			t.Fatal(err)
		}
		out = append(out, a)
	}
	return out
}

// seedDraw écrit un tirage actuel complet entre les personnes (giver offre à
// la personne suivante) : sert à vérifier qu'aucune page n'affiche une cible.
func (e *env) seedDraw(t *testing.T, gid int64, ids []int64) {
	t.Helper()
	res, err := e.db.Exec(`INSERT INTO draws (group_id, year, kind, status, gift_cap, drawn_at) VALUES (?, 2026, 'tirage', 'actuel', 2, ?)`, gid, t0.Format(time.RFC3339Nano))
	if err != nil {
		t.Fatal(err)
	}
	did, _ := res.LastInsertId()
	for i, g := range ids {
		if _, err := e.db.Exec(`INSERT INTO draw_pairs (draw_id, giver_id, receiver_id) VALUES (?, ?, ?)`, did, g, ids[(i+1)%len(ids)]); err != nil {
			t.Fatal(err)
		}
	}
	e.state.Done[gid] = true
}
