package letterpages

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"
	"time"

	"secretsanta/internal/config"
	"secretsanta/internal/db"
	"secretsanta/internal/draw"
	"secretsanta/internal/groups"
	"secretsanta/internal/i18n/catalog"
	"secretsanta/internal/i18n/negotiate"
	"secretsanta/internal/letter"
	"secretsanta/internal/letterread"
	"secretsanta/internal/participants"
	"secretsanta/internal/reveal"
	"secretsanta/internal/rules"
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

type sqlDirectory struct{ db *sql.DB }

func (d sqlDirectory) DrawRole(ctx context.Context, groupID, id int64) (string, bool, error) {
	var role string
	err := d.db.QueryRowContext(ctx, `SELECT draw_role FROM participants WHERE group_id = ? AND id = ?`, groupID, id).Scan(&role)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	return role, err == nil, err
}

type names struct{ parts *participants.Service }

func (n names) FirstName(ctx context.Context, groupID, id int64) (string, error) {
	p, err := n.parts.Get(ctx, participants.AdminActor(groupID), groupID, id)
	if err != nil {
		return "", err
	}
	return p.FirstName, nil
}

type env struct {
	db      *sql.DB
	clock   *clock.Fake
	groups  *groups.Service
	parts   *participants.Service
	draw    *draw.Service
	letters *letter.Service
	read    *letterread.Service
	sess    *websec.Sessions
	pages   *Pages
	handler http.Handler
	gid     int64
	slug    string
	revealA time.Time
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
	steps := []struct {
		module string
		load   func() ([]db.Migration, error)
	}{
		{groups.Module, groups.Migrations},
		{participants.Module, participants.Migrations},
		{rules.Module, rules.Migrations},
		{letter.Module, letter.Migrations},
		{draw.Module, draw.Migrations},
		{reveal.Module, reveal.Migrations},
		{letterread.Module, letterread.Migrations},
		{websec.MigrationModule, websec.Migrations},
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
	c := clock.NewFake(t0)
	g, err := groups.NewService(d, c, config.NewSecret("mot-de-passe-instance"))
	if err != nil {
		t.Fatal(err)
	}
	e := &env{db: d, clock: c, groups: g, slug: "noel", revealA: t0.Add(24 * time.Hour)}
	res, err := d.Exec(`INSERT INTO groups (slug, name, admin_secret_hash, created_at, max_gifts_per_giver, reveal_at)
		VALUES (?, 'Famille', zeroblob(32), ?, 2, ?)`, e.slug, t0.Format(time.RFC3339Nano), e.revealA.Format(time.RFC3339))
	if err != nil {
		t.Fatal(err)
	}
	e.gid, _ = res.LastInsertId()
	e.parts = participants.NewService(d, c, draw.NewState(d))
	r := rules.NewService(d, sqlDirectory{db: d})
	e.draw = draw.NewService(d, c, g, e.parts, r, &ports.FakeRegistrationState{Pending: map[int64]int{}},
		draw.Options{Gate: reveal.NewGate(d, c)})
	e.letters = letter.NewService(d, c, &ports.MemoryEventBus{}, t.TempDir())
	e.read = letterread.NewService(d, c, e.draw, e.letters)
	pol, err := websec.NewPolicy(publicURL)
	if err != nil {
		t.Fatal(err)
	}
	e.sess = websec.NewSessions(d, c, pol, websec.SessionOptions{})
	cat := realCatalog(t)
	e.pages, err = New(Options{Groups: g, Letters: e.letters, Reader: e.read, Targets: e.draw,
		Names: names{e.parts}, Translator: cat, Clock: c})
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	e.pages.Register(mux)
	chain := websec.NewCSRF(pol, websec.CSRFOptions{}).Middleware(e.sess.Middleware(mux))
	e.handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		lang := r.Header.Get("X-Test-Lang")
		if lang == "" {
			lang = "fr"
		}
		chain.ServeHTTP(w, r.WithContext(negotiate.WithLang(r.Context(), lang)))
	})
	return e
}

func (e *env) add(t *testing.T, first string, role participants.DrawRole) int64 {
	t.Helper()
	p, err := e.parts.Create(ctx, participants.AdminActor(e.gid), e.gid,
		participants.Input{FirstName: first, Name: "Zq" + first + "enko", Role: role})
	if err != nil {
		t.Fatalf("ajout de %s : %v", first, err)
	}
	return p.ID
}

// login ouvre une session participant et renvoie son cookie.
func (e *env) login(t *testing.T, pid int64) *http.Cookie {
	t.Helper()
	rec := httptest.NewRecorder()
	_, err := e.sess.Create(ctx, rec, nil, websec.Identity{Kind: websec.KindParticipant, GroupID: fmt.Sprint(e.gid), Subject: fmt.Sprint(pid)})
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range rec.Result().Cookies() {
		if c.Name == e.sess.CookieName(websec.KindParticipant) {
			return c
		}
	}
	t.Fatal("cookie de session absent")
	return nil
}

var csrfRe = regexp.MustCompile(`name="csrf_token" value="([^"]+)"`)

type reqOpt struct {
	cookie *http.Cookie
	lang   string
	form   url.Values
	// multipart : champs et fichier « image ».
	multipart map[string]string
	image     []byte
}

// do envoie une requête ; les POST portent le jeton et le cookie CSRF d'une
// visite préalable de la page d'écriture de la même personne.
func (e *env) do(t *testing.T, method, target string, o reqOpt) *httptest.ResponseRecorder {
	t.Helper()
	var cookies []*http.Cookie
	if o.cookie != nil {
		cookies = append(cookies, o.cookie)
	}
	token := ""
	if method == http.MethodPost {
		get := httptest.NewRequest(http.MethodGet, "/"+e.slug+"/lettre", nil)
		get.Header.Set("Sec-Fetch-Site", "same-origin")
		if o.cookie != nil {
			get.AddCookie(o.cookie)
		}
		rec := httptest.NewRecorder()
		e.handler.ServeHTTP(rec, get)
		m := csrfRe.FindStringSubmatch(rec.Body.String())
		if m == nil {
			t.Fatalf("jeton CSRF introuvable : %d", rec.Code)
		}
		token = m[1]
		cookies = append(cookies, rec.Result().Cookies()...)
	}
	var body io.Reader
	ct := ""
	switch {
	case o.multipart != nil:
		var buf bytes.Buffer
		mw := multipart.NewWriter(&buf)
		_ = mw.WriteField(websec.CSRFFieldName, token)
		for k, v := range o.multipart {
			_ = mw.WriteField(k, v)
		}
		if o.image != nil {
			fw, _ := mw.CreateFormFile("image", "photo.png")
			_, _ = fw.Write(o.image)
		}
		_ = mw.Close()
		body, ct = &buf, mw.FormDataContentType()
	case method == http.MethodPost:
		f := url.Values{}
		for k, v := range o.form {
			f[k] = v
		}
		f.Set(websec.CSRFFieldName, token)
		body, ct = strings.NewReader(f.Encode()), "application/x-www-form-urlencoded"
	}
	req := httptest.NewRequest(method, target, body)
	if ct != "" {
		req.Header.Set("Content-Type", ct)
	}
	req.Header.Set("Sec-Fetch-Site", "same-origin")
	if o.lang != "" {
		req.Header.Set("X-Test-Lang", o.lang)
	}
	for _, c := range cookies {
		req.AddCookie(c)
	}
	rec := httptest.NewRecorder()
	e.handler.ServeHTTP(rec, req)
	return rec
}

func (e *env) get(t *testing.T, target string, c *http.Cookie) *httptest.ResponseRecorder {
	return e.do(t, http.MethodGet, target, reqOpt{cookie: c})
}

func (e *env) mustDraw(t *testing.T) {
	t.Helper()
	if _, err := e.draw.Draw(ctx, participants.AdminActor(e.gid), e.gid, draw.Request{Confirmed: true}); err != nil {
		t.Fatalf("tirage refusé : %v", err)
	}
}

type rawPair struct{ ID, Giver, Receiver int64 }

func (e *env) pairs(t *testing.T) []rawPair {
	t.Helper()
	rows, err := e.db.Query(`SELECT p.id, p.giver_id, p.receiver_id FROM draw_pairs p JOIN draws d ON d.id = p.draw_id
		WHERE d.group_id = ? AND d.status = 'actuel' ORDER BY p.id`, e.gid)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []rawPair
	for rows.Next() {
		var p rawPair
		if err := rows.Scan(&p.ID, &p.Giver, &p.Receiver); err != nil {
			t.Fatal(err)
		}
		out = append(out, p)
	}
	return out
}

// revealed tire au sort, ouvre la révélation et découvre toutes les cibles.
func (e *env) revealed(t *testing.T) []rawPair {
	t.Helper()
	e.mustDraw(t)
	e.clock.Set(e.revealA)
	ps := e.pairs(t)
	for _, p := range ps {
		if _, err := e.draw.MarkDiscovered(ctx, participants.ParticipantActor(e.gid, p.Giver), e.gid, p.ID); err != nil {
			t.Fatalf("découverte : %v", err)
		}
	}
	return ps
}

func pngBytes(t *testing.T) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 8, 8))
	img.Set(1, 1, color.RGBA{R: 200, A: 255})
	var b bytes.Buffer
	if err := png.Encode(&b, img); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func (e *env) letterVersion(t *testing.T, pid int64) int {
	t.Helper()
	l, err := e.letters.Get(ctx, e.gid, pid)
	if err != nil {
		t.Fatal(err)
	}
	return l.Version
}

func itoa(n int64) string { return fmt.Sprint(n) }

func ideaIn(title string) letter.IdeaInput { return letter.IdeaInput{Title: title} }
