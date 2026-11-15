package admin

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
	"regexp"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"secretsanta/internal/db"
	"secretsanta/internal/groups"
	"secretsanta/internal/i18n/catalog"
	"secretsanta/internal/i18n/negotiate"
	i18nweb "secretsanta/internal/i18n/web"
	"secretsanta/internal/participants"
	"secretsanta/internal/registration/code"
	"secretsanta/internal/registration/invite"
	"secretsanta/internal/registration/verify"
	"secretsanta/internal/shared/adminnav"
	"secretsanta/internal/shared/clock"
	"secretsanta/internal/shared/errs"
	"secretsanta/internal/websec"
	"secretsanta/web"
)

const testOrigin = "http://127.0.0.1:8080"

type fakeGroups struct{ byslug map[string]*groups.Group }

func (f fakeGroups) BySlug(_ context.Context, slug string) (*groups.Group, error) {
	if g, ok := f.byslug[slug]; ok {
		return g, nil
	}
	return nil, errs.NotFound("groupe inconnu")
}

// fakeInvite garde l'état du mode par groupe et note les appels qui modifient.
type fakeInvite struct {
	mu      sync.Mutex
	enabled map[int64]bool
	cap     map[int64]int
	closes  map[int64]*time.Time
	role    map[int64]bool
	calls   []string
}

func newFakeInvite() *fakeInvite {
	return &fakeInvite{enabled: map[int64]bool{}, cap: map[int64]int{}, closes: map[int64]*time.Time{}, role: map[int64]bool{}}
}

func (f *fakeInvite) note(s string) { f.calls = append(f.calls, s) }

func (f *fakeInvite) Settings(_ context.Context, _ participants.Actor, g int64) (invite.Settings, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return invite.Settings{Enabled: f.enabled[g], Capacity: f.cap[g], ClosesAt: f.closes[g], RoleChoice: f.role[g]}, nil
}

func (f *fakeInvite) Enable(_ context.Context, _ participants.Actor, g int64) (*invite.Link, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.note("enable")
	f.enabled[g] = true
	return &invite.Link{Token: "tok1", Path: "/famille/rejoindre/tok1"}, nil
}

func (f *fakeInvite) Renew(_ context.Context, _ participants.Actor, g int64) (*invite.Link, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.note("renew")
	return &invite.Link{Token: "tok2", Path: "/famille/rejoindre/tok2"}, nil
}

func (f *fakeInvite) Disable(_ context.Context, _ participants.Actor, g int64) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.note("disable")
	f.enabled[g] = false
	return nil
}

func (f *fakeInvite) SetLimits(_ context.Context, _ participants.Actor, g int64, c int, t *time.Time) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.note("limits")
	if c < 0 || c > invite.MaxCapacity {
		return errs.Validation("capacité")
	}
	f.cap[g], f.closes[g] = c, t
	return nil
}

func (f *fakeInvite) SetRoleChoice(_ context.Context, _ participants.Actor, g int64, on bool) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.note("role")
	f.role[g] = on
	return nil
}

func (f *fakeInvite) callCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.calls)
}

type fakeVerify struct {
	mu      sync.Mutex
	pending map[int64][]verify.PendingView
	calls   []string
	resend  error
}

func (f *fakeVerify) ListPending(_ context.Context, _ participants.Actor, g int64) ([]verify.PendingView, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]verify.PendingView(nil), f.pending[g]...), nil
}

func (f *fakeVerify) ValidateWithoutCode(_ context.Context, _ participants.Actor, g, id int64) (*verify.AdminValidation, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, "validate:"+strconv.FormatInt(id, 10))
	return &verify.AdminValidation{RegistrationID: id}, nil
}

func (f *fakeVerify) ResendCode(_ context.Context, _ participants.Actor, g, id int64) (code.Issued, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, "resend:"+strconv.FormatInt(id, 10))
	return code.Issued{}, f.resend
}

func (f *fakeVerify) DeleteRegistration(_ context.Context, _ participants.Actor, g, id int64) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, "delete:"+strconv.FormatInt(id, 10))
	return nil
}

func (f *fakeVerify) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.calls)
}

type fakeCount struct{}

func (fakeCount) Count(context.Context, int64) (int, error) { return 6, nil }

type fakeDraw struct{ done bool }

func (f *fakeDraw) IsDrawDone(context.Context, int64) (bool, error) { return f.done, nil }

type env struct {
	pages   *Pages
	handler http.Handler
	inv     *fakeInvite
	ver     *fakeVerify
	draw    *fakeDraw
	clock   *clock.Fake
	cookies map[string][]*http.Cookie
	// lang est la langue des requêtes envoyées par post.
	lang string
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
	tm, err := tm.ParseFS(web.Templates, "templates/layout/*.html", "templates/i18n/*.html", "templates/registration/admin/*.html")
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
	clk := clock.NewFake(time.Date(2026, 11, 15, 10, 0, 0, 0, time.UTC))
	sess := websec.NewSessions(d, clk, pol, websec.SessionOptions{})
	reg := adminnav.New()
	DeclareNav(reg)
	g1 := &groups.Group{ID: 1, Slug: "famille", Name: "Famille"}
	g2 := &groups.Group{ID: 2, Slug: "amis", Name: "Amis"}
	cat := realCatalog(t)
	inv, ver, dr := newFakeInvite(), &fakeVerify{pending: map[int64][]verify.PendingView{
		1: {{ID: 7, FirstName: "Jeanne", Name: "Dubois", Email: "jeanne@exemple.fr",
			CreatedAt: clk.Now().Add(-3 * time.Hour), ExpiresAt: clk.Now().Add(45 * time.Hour)}},
		2: {{ID: 9, FirstName: "Autre", Name: "Groupe", Email: "autre@exemple.fr",
			CreatedAt: clk.Now().Add(-time.Hour), ExpiresAt: clk.Now().Add(47 * time.Hour)}},
	}}, &fakeDraw{}
	p, err := New(Deps{Groups: fakeGroups{map[string]*groups.Group{"famille": g1, "amis": g2}}, Invite: inv, Verify: ver,
		Counter: fakeCount{}, Draw: dr, Render: realRenderer(t, cat), Nav: reg, Clock: clk,
		PublicURL: testOrigin, QR: func(s string) ([]byte, error) { return []byte("PNG:" + s), nil }})
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	p.Register(mux)
	e := &env{pages: p, inv: inv, ver: ver, draw: dr, clock: clk, cookies: map[string][]*http.Cookie{}}
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

var csrfRe = regexp.MustCompile(`name="csrf_token" value="([^"]+)"`)

// post charge la page pour obtenir un jeton CSRF puis envoie le formulaire.
func (e *env) post(t *testing.T, slug, target string, form url.Values) *httptest.ResponseRecorder {
	t.Helper()
	page := e.do("GET", "/"+slug+"/admin/inscriptions", "", nil, e.cookies[slug])
	m := csrfRe.FindStringSubmatch(page.Body.String())
	if m == nil {
		t.Fatalf("jeton CSRF absent (statut %d)", page.Code)
	}
	cookies := append(append([]*http.Cookie{}, e.cookies[slug]...), page.Result().Cookies()...)
	f := url.Values{"csrf_token": {strings.ReplaceAll(m[1], "&#43;", "+")}}
	for k, v := range form {
		f[k] = v
	}
	return e.do("POST", target, e.lang, f, cookies)
}

func catalogFile(t *testing.T, lang string) map[string]string {
	t.Helper()
	raw, err := web.I18n.ReadFile("static/i18n/registration/admin/" + lang + ".json")
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]string{}
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatal(err)
	}
	return out
}

func errTooSoon() error { return code.ErrTooSoon }
