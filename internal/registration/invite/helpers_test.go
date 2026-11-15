package invite

import (
	"context"
	"database/sql"
	"fmt"
	"html"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"secretsanta/internal/access/magic"
	"secretsanta/internal/config"
	"secretsanta/internal/db"
	"secretsanta/internal/groups"
	"secretsanta/internal/participants"
	"secretsanta/internal/shared/clock"
	"secretsanta/internal/shared/ports"
	"secretsanta/internal/websec"
)

var t0 = time.Date(2026, 11, 15, 10, 0, 0, 0, time.UTC)

const publicURL = "http://127.0.0.1:8080"

// codes enregistre les inscriptions pour lesquelles un code a été émis.
type codes struct {
	mu     sync.Mutex
	issued []Pending
	err    error
}

func (c *codes) IssueCode(_ context.Context, p Pending) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.err != nil {
		return c.err
	}
	c.issued = append(c.issued, p)
	return nil
}

func (c *codes) count() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.issued)
}

type env struct {
	db     *sql.DB
	clock  *clock.Fake
	draw   *ports.FakeDrawState
	store  *MemoryStore
	codes  *codes
	parts  *participants.Service
	svc    *Service
	g1, g2 int64
}

func insertGroup(t *testing.T, d *sql.DB, slug string) int64 {
	t.Helper()
	res, err := d.Exec(`INSERT INTO groups (slug, name, admin_secret_hash, created_at) VALUES (?, ?, zeroblob(32), ?)`,
		slug, "Groupe "+slug, t0.Format(time.RFC3339))
	if err != nil {
		t.Fatal(err)
	}
	id, _ := res.LastInsertId()
	return id
}

func newEnv(t *testing.T) *env {
	t.Helper()
	d, err := db.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = d.Close() })
	gm, err := groups.Migrations()
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Migrate(d, groups.Module, gm); err != nil {
		t.Fatal(err)
	}
	pm, err := participants.Migrations()
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Migrate(d, participants.Module, pm); err != nil {
		t.Fatal(err)
	}
	c := clock.NewFake(t0)
	gs, err := groups.NewService(d, c, config.Secret{})
	if err != nil {
		t.Fatal(err)
	}
	e := &env{db: d, clock: c, draw: &ports.FakeDrawState{Done: map[int64]bool{}}, store: NewMemoryStore(), codes: &codes{}}
	e.parts = participants.NewService(d, c, e.draw)
	e.svc, err = New(Config{
		Groups: gs, Directory: SQLDirectory{DB: d}, Store: e.store, Draw: e.draw, Clock: c,
		Key: []byte("cle-de-signature-de-test-0123456789"), Codes: e.codes,
	})
	if err != nil {
		t.Fatal(err)
	}
	e.g1 = insertGroup(t, d, "noel")
	e.g2 = insertGroup(t, d, "famille")
	return e
}

func admin(g int64) participants.Actor { return participants.AdminActor(g) }

func (e *env) enable(t *testing.T, g int64) *Link {
	t.Helper()
	l, err := e.svc.Enable(t.Context(), admin(g), g)
	if err != nil {
		t.Fatal(err)
	}
	return l
}

func (e *env) addParticipant(t *testing.T, g int64, first, name, email string) {
	t.Helper()
	if _, err := e.parts.Create(t.Context(), admin(g), g, participants.Input{FirstName: first, Name: name, Email: email}); err != nil {
		t.Fatal(err)
	}
}

func (e *env) pendingCount(t *testing.T, g int64) int {
	t.Helper()
	n, err := e.store.CountPending(t.Context(), g, e.clock.Now())
	if err != nil {
		t.Fatal(err)
	}
	return n
}

func (e *env) submit(t *testing.T, slug, tok string, sub Submission) *Result {
	t.Helper()
	r, err := e.svc.Submit(t.Context(), slug, tok, sub)
	if err != nil {
		t.Fatalf("soumission : %v", err)
	}
	return r
}

func sub(first, name, email string) Submission {
	return Submission{FirstName: first, Name: name, Email: email, IP: "203.0.113.7"}
}

// pageRenderer est un rendu minimal : formulaire avec jeton CSRF, issue et
// messages. Il écrit tout ce que la vue contient, pour qu'un test puisse
// vérifier qu'aucun nom de participant n'y figure.
type pageRenderer struct{}

func (pageRenderer) Render(w http.ResponseWriter, r *http.Request, status int, v View) {
	field, err := websec.Field(r)
	if err != nil {
		field = ""
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	var b strings.Builder
	fmt.Fprintf(&b, "<main><h1>%s</h1>", html.EscapeString(v.Invite.Name))
	if v.Closed != "" {
		_, msg := v.Closed.Message()
		fmt.Fprintf(&b, `<p class="closed">%s</p>`, html.EscapeString(msg))
	}
	if v.Result != nil {
		fmt.Fprintf(&b, `<p class="outcome">%s</p><p>%s</p>`, v.Result.Outcome, html.EscapeString(v.Result.Problem.Message))
		for k, p := range v.Result.Fields {
			fmt.Fprintf(&b, `<p class="field-error" data-field="%s">%s</p>`, k, html.EscapeString(p.Message))
		}
		for _, o := range v.Result.Offers {
			fmt.Fprintf(&b, `<a href="%s">%s</a>`, html.EscapeString(o.Path), html.EscapeString(o.Message))
		}
	}
	if v.Closed == "" {
		fmt.Fprintf(&b, `<form method="post" action="%s">%s<input name="prenom"><input name="nom"><input name="email"></form>`,
			html.EscapeString(v.Action), field)
	}
	b.WriteString("</main>")
	_, _ = w.Write([]byte(b.String()))
}

// server monte /<slug>/rejoindre/<jeton> derrière le middleware CSRF (sans
// SecurityHeaders : l'en-tête no-referrer doit venir de l'inscription).
func (e *env) server(t *testing.T) http.Handler {
	t.Helper()
	pol, err := websec.NewPolicy(publicURL)
	if err != nil {
		t.Fatal(err)
	}
	h := &Handlers{Service: e.svc, Render: pageRenderer{}}
	mux := http.NewServeMux()
	mux.Handle(magic.PatternJoin, h.Join())
	mux.Handle(magic.PatternJoinBare, h.Join())
	return websec.NewCSRF(pol, websec.CSRFOptions{}).Middleware(mux)
}

var csrfRe = regexp.MustCompile(`name="csrf_token" value="([^"]+)"`)

// get envoie un GET.
func get(h http.Handler, path string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	return rec
}

// postForm ouvre la page (cookie et jeton CSRF) puis soumet le formulaire.
func postForm(t *testing.T, h http.Handler, path string, form url.Values) *httptest.ResponseRecorder {
	t.Helper()
	page := get(h, path)
	m := csrfRe.FindStringSubmatch(page.Body.String())
	if m == nil {
		t.Fatalf("jeton CSRF introuvable : %d %s", page.Code, page.Body.String())
	}
	form.Set(websec.CSRFFieldName, html.UnescapeString(m[1]))
	return rawPost(h, path, form, page.Result().Cookies(), true)
}

// rawPost soumet sans passer par la page ; sameOrigin pose Sec-Fetch-Site.
func rawPost(h http.Handler, path string, form url.Values, cookies []*http.Cookie, sameOrigin bool) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if sameOrigin {
		req.Header.Set("Sec-Fetch-Site", "same-origin")
	}
	for _, c := range cookies {
		req.AddCookie(c)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func formOf(first, name, email string) url.Values {
	return url.Values{FieldFirstName: {first}, FieldName: {name}, FieldEmail: {email}}
}
