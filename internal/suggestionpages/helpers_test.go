package suggestionpages

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"html"
	"html/template"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"secretsanta/internal/db"
	"secretsanta/internal/draw"
	"secretsanta/internal/groups"
	"secretsanta/internal/i18n/catalog"
	"secretsanta/internal/i18n/negotiate"
	i18nweb "secretsanta/internal/i18n/web"
	"secretsanta/internal/participants"
	"secretsanta/internal/shared/clock"
	"secretsanta/internal/shared/errs"
	"secretsanta/internal/suggestions"
	"secretsanta/internal/websec"
	"secretsanta/web"
)

const testOrigin = "http://127.0.0.1:8080"

var (
	ctx = context.Background()
	t0  = time.Date(2026, 11, 15, 10, 0, 0, 0, time.UTC)
)

// Les personnes de la scène : 1 = tireur à deux cibles (offre_plus), 2 =
// tireur normal à une cible, 3 = réception seule, 10 et 11 = cibles.
const (
	giver2   = int64(1)
	giver1   = int64(2)
	recepOnl = int64(3)
	gid      = int64(7)
)

type fakeGroups struct{ g groups.Group }

func (f *fakeGroups) BySlug(_ context.Context, slug string) (*groups.Group, error) {
	if slug != f.g.Slug {
		return nil, notFound()
	}
	g := f.g
	return &g, nil
}

type fakePeople struct{}

func (fakePeople) Get(_ context.Context, _ participants.Actor, _, id int64) (*participants.Participant, error) {
	names := map[int64]string{giver2: "Alix", giver1: "Bastien", recepOnl: "Cléo", 10: "Léa", 11: "Noé", 12: "Zoé"}
	role := participants.RoleNormal
	switch id {
	case giver2:
		role = participants.RoleOffrePlus
	case recepOnl:
		role = participants.RoleReceptionSeule
	}
	n, ok := names[id]
	if !ok {
		return nil, notFound()
	}
	return &participants.Participant{ID: id, FirstName: n, Name: n + " Test", Role: role}, nil
}

type fakeDraw struct {
	ready   bool
	targets map[int64][]draw.Target
}

func (f *fakeDraw) MyTargets(_ context.Context, a participants.Actor, _ int64) (*draw.MyTargets, error) {
	return &draw.MyTargets{Done: true, Ready: f.ready, Targets: f.targets[a.ParticipantID]}, nil
}

type fakeSuggest struct {
	mu    sync.Mutex
	calls []suggestions.Request
	res   *suggestions.Result
	err   error
}

func (f *fakeSuggest) Suggest(_ context.Context, r suggestions.Request) (*suggestions.Result, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, r)
	if f.err != nil {
		return nil, f.err
	}
	res := *f.res
	return &res, nil
}

func (f *fakeSuggest) count() int { f.mu.Lock(); defer f.mu.Unlock(); return len(f.calls) }
func (f *fakeSuggest) last() suggestions.Request {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls[len(f.calls)-1]
}

type env struct {
	db      *sql.DB
	groups  *fakeGroups
	draw    *fakeDraw
	suggest *fakeSuggest
	sess    *websec.Sessions
	pages   *Pages
	handler http.Handler
	jar     map[string]*http.Cookie
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
	tm, err := tm.ParseFS(web.Templates, "templates/layout/*.html", "templates/i18n/*.html", "templates/suggestions/*.html")
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

func goodResult() *suggestions.Result {
	return &suggestions.Result{Items: []suggestions.Item{
		{Title: "Écharpe douce, carreaux verts", URL: "https://boutique.example/echarpe", ImageSrc: "/images/abc.jpg", PriceCents: 2499, Source: "searxng"},
		{Title: "Mug sapin", URL: "https://boutique.example/mug"},
	}}
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
	e := &env{db: d, jar: map[string]*http.Cookie{}}
	e.groups = &fakeGroups{g: groups.Group{ID: gid, Slug: "famille", Name: "Famille"}}
	e.draw = &fakeDraw{ready: true, targets: map[int64][]draw.Target{
		giver2:   {{PairID: 101, ReceiverID: 10, Discovered: true}, {PairID: 102, ReceiverID: 11, Discovered: true}},
		giver1:   {{PairID: 103, ReceiverID: 12, Discovered: true}},
		recepOnl: nil,
	}}
	e.suggest = &fakeSuggest{res: goodResult()}
	pol, err := websec.NewPolicy(testOrigin)
	if err != nil {
		t.Fatal(err)
	}
	e.sess = websec.NewSessions(d, clock.NewFake(t0), pol, websec.SessionOptions{})
	e.pages, err = New(Deps{Groups: e.groups, Participants: fakePeople{}, Draw: e.draw, Suggest: e.suggest, Render: realRenderer(t, realCatalog(t))})
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	mux.Handle("GET /{slug}/suggestions/{pair}", e.pages.Show())
	mux.Handle("/{slug}/suggestions/{pair}/actualiser", e.pages.Refresh())
	mux.HandleFunc("GET /{slug}/_token", func(w http.ResponseWriter, r *http.Request) {
		tok, err := websec.Token(r)
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		_, _ = w.Write([]byte(tok))
	})
	e.handler = websec.NewCSRF(pol, websec.CSRFOptions{}).Middleware(e.sess.Middleware(mux))
	return e
}

func (e *env) login(t *testing.T, pid int64) {
	t.Helper()
	rec := httptest.NewRecorder()
	if _, err := e.sess.Create(ctx, rec, httptest.NewRequest("POST", "/", nil), websec.Identity{
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

// post charge d'abord un jeton CSRF, puis envoie le formulaire.
func (e *env) post(t *testing.T, path, lang string, form url.Values) *httptest.ResponseRecorder {
	t.Helper()
	w := e.do("GET", "/famille/_token", lang, nil)
	if w.Code != 200 {
		t.Fatalf("jeton CSRF absent (%d)", w.Code)
	}
	if form == nil {
		form = url.Values{}
	}
	form.Set("csrf_token", w.Body.String())
	return e.do("POST", path, lang, form)
}

func catalogFile(t *testing.T, lang string) map[string]string {
	t.Helper()
	raw, err := web.I18n.ReadFile("static/i18n/suggestions/" + lang + ".json")
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

func notFound() error { return errs.NotFound("introuvable") }
