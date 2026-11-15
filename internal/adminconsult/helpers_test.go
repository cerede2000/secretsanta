package adminconsult

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
	"secretsanta/internal/participants"
	"secretsanta/internal/rules"
	"secretsanta/internal/shared/clock"
	"secretsanta/internal/shared/ports"
	"secretsanta/internal/websec"
	"secretsanta/web"
)

const publicURL = "http://127.0.0.1:8080"

var (
	t0  = time.Date(2026, 12, 12, 10, 0, 0, 0, time.UTC)
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

type env struct {
	t        *testing.T
	db       *sql.DB
	clock    *clock.Fake
	groups   *groups.Service
	parts    *participants.Service
	draws    *draw.Service
	svc      *Service
	pages    *Pages
	sessions *websec.Sessions
	handler  http.Handler
	secrets  map[int64]string
	names    map[int64][]string
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
		{Module, Migrations},
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
	e := &env{t: t, db: d, clock: clk, groups: gs, secrets: map[int64]string{}, names: map[int64][]string{}}
	e.parts = participants.NewService(d, clk, draw.NewState(d))
	rs := rules.NewService(d, sqlDirectory{db: d})
	e.draws = draw.NewService(d, clk, gs, e.parts, rs, &ports.FakeRegistrationState{Pending: map[int64]int{}}, draw.Options{})
	if e.svc, err = NewService(d, clk, gs, e.parts); err != nil {
		t.Fatal(err)
	}
	cat, err := catalog.Load(web.I18n, catalog.Options{DefaultLang: "fr", Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})
	if err != nil {
		t.Fatal(err)
	}
	if e.pages, err = NewPages(PageOptions{Service: e.svc, Groups: gs, Translator: cat}); err != nil {
		t.Fatal(err)
	}
	pol, err := websec.NewPolicy(publicURL)
	if err != nil {
		t.Fatal(err)
	}
	e.sessions = websec.NewSessions(d, clk, pol, websec.SessionOptions{})
	mux := http.NewServeMux()
	e.pages.Register(mux)
	// Route de test : donne un jeton CSRF valable, même quand aucune page de
	// consultation n'est servie (option désactivée).
	mux.HandleFunc("GET /__test/jeton", func(w http.ResponseWriter, r *http.Request) {
		tok, err := websec.Token(r)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		_, _ = io.WriteString(w, tok)
	})
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

// group crée un groupe par le service réel (secret d'administration connu).
func (e *env) group(slug string, canView bool) int64 {
	e.t.Helper()
	res, err := e.groups.Create(ctx, groups.CreateInput{InstancePassword: "mot-de-passe-instance", ClientKey: "test",
		Name: "Groupe " + slug, Slug: slug, AdminCanViewPair: canView})
	if err != nil {
		e.t.Fatalf("création de %s : %v", slug, err)
	}
	e.secrets[res.Group.ID] = res.AdminSecret
	return res.Group.ID
}

func (e *env) add(gid int64, first string, role participants.DrawRole) int64 {
	e.t.Helper()
	name := "Zx" + first + "ovitch"
	p, err := e.parts.Create(ctx, participants.AdminActor(gid), gid, participants.Input{FirstName: first, Name: name, Role: role})
	if err != nil {
		e.t.Fatalf("ajout de %s : %v", first, err)
	}
	e.names[p.ID] = []string{first, name}
	return p.ID
}

func (e *env) addN(gid int64, prefix string, n int) []int64 {
	var ids []int64
	for i := 0; i < n; i++ {
		ids = append(ids, e.add(gid, fmt.Sprintf("%s%c", prefix, 'a'+i), participants.RoleNormal))
	}
	return ids
}

func (e *env) doDraw(gid int64) {
	e.t.Helper()
	if _, err := e.draws.Draw(ctx, participants.AdminActor(gid), gid, draw.Request{Confirmed: true}); err != nil {
		e.t.Fatalf("tirage : %v", err)
	}
}

// receiversOf lit la vérité en base (pour comparer au résultat affiché).
func (e *env) receiversOf(gid, giver int64) []int64 {
	e.t.Helper()
	rows, err := e.db.Query(`SELECT p.receiver_id FROM draw_pairs p JOIN draws d ON d.id = p.draw_id
		WHERE d.group_id = ? AND d.status = 'actuel' AND p.giver_id = ? ORDER BY p.id`, gid, giver)
	if err != nil {
		e.t.Fatal(err)
	}
	defer rows.Close()
	var out []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			e.t.Fatal(err)
		}
		out = append(out, id)
	}
	return out
}

func (e *env) journalRows(gid int64) int {
	return countRows(e.t, e.db, `SELECT COUNT(*) FROM adminconsult_journal WHERE group_id = ?`, gid)
}

func (e *env) cookie(id websec.Identity) *http.Cookie {
	e.t.Helper()
	rec := httptest.NewRecorder()
	if _, err := e.sessions.Create(ctx, rec, nil, id); err != nil {
		e.t.Fatal(err)
	}
	for _, c := range rec.Result().Cookies() {
		if c.Name == e.sessions.CookieName(id.Kind) {
			return c
		}
	}
	e.t.Fatal("cookie de session absent")
	return nil
}

func (e *env) adminCookie(gid int64) *http.Cookie {
	return e.cookie(websec.Identity{Kind: websec.KindAdmin, GroupID: strconv.FormatInt(gid, 10)})
}

func (e *env) participantCookie(gid, pid int64) *http.Cookie {
	return e.cookie(websec.Identity{Kind: websec.KindParticipant, GroupID: strconv.FormatInt(gid, 10), Subject: strconv.FormatInt(pid, 10)})
}

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

// postWithToken envoie un POST avec un jeton CSRF déjà connu.
func (e *env) postWithToken(path, lang, token string, ck *http.Cookie, extra []*http.Cookie, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Sec-Fetch-Site", "same-origin")
	req.Header.Set("X-Test-Lang", lang)
	if token != "" {
		req.Header.Set("X-CSRF-Token", token)
	}
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

// token renvoie un jeton CSRF valable et les cookies qui vont avec.
func (e *env) token(ck *http.Cookie) (string, []*http.Cookie) {
	e.t.Helper()
	page := e.get("/__test/jeton", "fr", ck)
	if page.Code != http.StatusOK || page.Body.Len() == 0 {
		e.t.Fatalf("jeton CSRF : %d", page.Code)
	}
	var extra []*http.Cookie
	for _, c := range page.Result().Cookies() {
		if ck == nil || c.Name != ck.Name {
			extra = append(extra, c)
		}
	}
	return page.Body.String(), extra
}

// consult envoie le formulaire de consultation (jeton CSRF valable).
func (e *env) consult(slug, lang string, ck *http.Cookie, fields url.Values) *httptest.ResponseRecorder {
	e.t.Helper()
	tok, extra := e.token(ck)
	form := url.Values{websec.CSRFFieldName: {tok}}
	for k, v := range fields {
		form[k] = v
	}
	return e.postWithToken("/"+slug+"/admin/consultation", lang, "", ck, extra, form.Encode())
}

func countRows(t *testing.T, d *sql.DB, query string, args ...any) int {
	t.Helper()
	var n int
	if err := d.QueryRow(query, args...).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func idStr(id int64) string { return strconv.FormatInt(id, 10) }
