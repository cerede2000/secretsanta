package dashboard

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
	"testing"
	"time"

	"secretsanta/internal/admin/audit"
	"secretsanta/internal/config"
	"secretsanta/internal/db"
	"secretsanta/internal/draw"
	"secretsanta/internal/groups"
	"secretsanta/internal/i18n/catalog"
	"secretsanta/internal/i18n/negotiate"
	"secretsanta/internal/letter"
	"secretsanta/internal/mail/migrations"
	"secretsanta/internal/participants"
	"secretsanta/internal/reveal"
	"secretsanta/internal/rules"
	"secretsanta/internal/shared/adminnav"
	"secretsanta/internal/shared/clock"
	"secretsanta/internal/shared/ports"
	"secretsanta/internal/websec"
	"secretsanta/web"
)

const publicURL = "http://127.0.0.1:8080"

var (
	t0  = time.Date(2026, 11, 15, 10, 0, 0, 0, time.UTC)
	ctx = context.Background()
)

// sqlDirectory adapte la table participants au port rules.Directory.
type sqlDirectory struct{ db *sql.DB }

func (d sqlDirectory) DrawRole(ctx context.Context, groupID, id int64) (string, bool, error) {
	var role string
	err := d.db.QueryRowContext(ctx, `SELECT draw_role FROM participants WHERE group_id = ? AND id = ?`, groupID, id).Scan(&role)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	return role, err == nil, err
}

// fakeReveal remplace le service de révélation.
type fakeReveal struct{ open bool }

func (f *fakeReveal) Facts(_ context.Context, a participants.Actor, groupID int64) (*reveal.Facts, error) {
	return &reveal.Facts{DrawDone: true, Open: f.open}, nil
}

type env struct {
	t        *testing.T
	db       *sql.DB
	clock    *clock.Fake
	reg      *ports.FakeRegistrationState
	groups   *groups.Service
	parts    *participants.Service
	rules    *rules.Service
	draws    *draw.Service
	audit    *audit.Service
	reveal   *fakeReveal
	svc      *Service
	pages    *Pages
	sessions *websec.Sessions
	cat      *catalog.Catalog
	handler  http.Handler
	names    []string
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
		{letter.Module, letter.Migrations},
		{migrations.Module, migrations.Migrations},
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
	e.rules = rules.NewService(d, sqlDirectory{db: d})
	e.draws = draw.NewService(d, clk, gs, e.parts, e.rules, e.reg, draw.Options{})
	e.audit = audit.New(d, clk)
	e.svc, err = New(Options{DB: d, Clock: clk, Participants: e.parts, Draws: e.draws, Constraints: e.rules,
		Audit: e.audit, Reveal: e.reveal, Registrations: e.reg})
	if err != nil {
		t.Fatal(err)
	}
	cat, err := catalog.Load(web.I18n, catalog.Options{DefaultLang: "fr", Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})
	if err != nil {
		t.Fatal(err)
	}
	e.cat = cat
	reg := adminnav.New()
	reg.Declare(adminnav.Entry{Key: "dashboard", Label: "layout.nav.dashboard", Icon: "dashboard", Rank: adminnav.RankDashboard, Destination: "/x/admin"})
	e.pages, err = NewPages(PageOptions{Service: e.svc, Groups: gs, Translator: cat,
		Nav: func(active string) []adminnav.Item { return reg.Items(active) }})
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

// group crée un groupe (plafond de cadeaux 2) et renvoie son identifiant.
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

// add ajoute une personne au nom reconnaissable ; email vide : sans courriel.
func (e *env) add(gid int64, first, email string, role participants.DrawRole) int64 {
	e.t.Helper()
	name := "Zx" + first + "ovitch"
	p, err := e.parts.Create(ctx, participants.AdminActor(gid), gid, participants.Input{FirstName: first, Name: name, Email: email, Role: role})
	if err != nil {
		e.t.Fatalf("ajout de %s : %v", first, err)
	}
	e.names = append(e.names, first, name)
	return p.ID
}

func (e *env) addN(gid int64, prefix string, n int, role participants.DrawRole) []int64 {
	var ids []int64
	for i := 0; i < n; i++ {
		ids = append(ids, e.add(gid, fmt.Sprintf("%s%c", prefix, 'a'+i), "", role))
	}
	return ids
}

func (e *env) doDraw(gid int64) {
	e.t.Helper()
	if _, err := e.draws.Draw(ctx, participants.AdminActor(gid), gid, draw.Request{Confirmed: true}); err != nil {
		e.t.Fatalf("tirage : %v", err)
	}
}

// pairs lit les affectations du tirage actuel (pour les vérifications).
func (e *env) pairs(gid int64) [][2]int64 {
	e.t.Helper()
	rows, err := e.db.Query(`SELECT p.giver_id, p.receiver_id FROM draw_pairs p JOIN draws d ON d.id = p.draw_id
		WHERE d.group_id = ? AND d.status = 'actuel' ORDER BY p.id`, gid)
	if err != nil {
		e.t.Fatal(err)
	}
	defer rows.Close()
	var out [][2]int64
	for rows.Next() {
		var p [2]int64
		if err := rows.Scan(&p[0], &p[1]); err != nil {
			e.t.Fatal(err)
		}
		out = append(out, p)
	}
	return out
}

func (e *env) mail(gid, pid int64, status string) {
	e.t.Helper()
	now := t0.UnixNano()
	if _, err := e.db.Exec(`INSERT INTO mail_queue (group_id, participant_id, recipient, mail_type, status, created_at, updated_at, send_after, sent_at)
		VALUES (?, ?, 'x@exemple.fr', 'invitation', ?, ?, ?, ?, ?)`, gid, pid, status, now, now, now, now); err != nil {
		e.t.Fatal(err)
	}
}

// adminCookie ouvre une session administrateur du groupe.
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

// get envoie un GET ; ck peut être nil.
func (e *env) get(path, lang string, ck *http.Cookie, extra ...*http.Cookie) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, path, nil)
	req.Header.Set("X-Test-Lang", lang)
	if ck != nil {
		req.AddCookie(ck)
	}
	for _, c := range extra {
		req.AddCookie(c)
	}
	rec := httptest.NewRecorder()
	e.handler.ServeHTTP(rec, req)
	return rec
}

// post envoie un POST avec le jeton CSRF pris sur la page tokenPage (qui doit
// porter un formulaire), vu avec la session ck.
func (e *env) post(path, tokenPage string, ck *http.Cookie) *httptest.ResponseRecorder {
	e.t.Helper()
	page := e.get(tokenPage, "fr", ck)
	m := csrfRe.FindStringSubmatch(page.Body.String())
	if m == nil {
		e.t.Fatalf("jeton CSRF introuvable sur %s : %d", tokenPage, page.Code)
	}
	form := url.Values{websec.CSRFFieldName: {m[1]}}
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

func countRows(t *testing.T, d *sql.DB, query string, args ...any) int {
	t.Helper()
	var n int
	if err := d.QueryRow(query, args...).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func admin(gid int64) participants.Actor { return participants.AdminActor(gid) }

func itoa(n int64) string { return strconv.FormatInt(n, 10) }
