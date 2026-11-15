package adjustpages

import (
	"context"
	"encoding/json"
	"io"
	"io/fs"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"secretsanta/internal/adjust"
	"secretsanta/internal/config"
	"secretsanta/internal/db"
	"secretsanta/internal/drawengine"
	"secretsanta/internal/groups"
	"secretsanta/internal/i18n/catalog"
	"secretsanta/internal/i18n/negotiate"
	"secretsanta/internal/participants"
	"secretsanta/internal/shared/clock"
	"secretsanta/internal/shared/errs"
	"secretsanta/internal/websec"
	"secretsanta/web"
)

const publicURL = "http://127.0.0.1:8080"

var t0 = time.Date(2026, 11, 15, 10, 0, 0, 0, time.UTC)

type fakeAdjust struct {
	preview  *adjust.Preview
	result   *adjust.Result
	applyErr error
	applied  []adjust.Change
	lastConf adjust.Confirmation
}

func (f *fakeAdjust) Preview(_ context.Context, _ participants.Actor, _ int64, _ adjust.Change) (*adjust.Preview, error) {
	p := *f.preview
	return &p, nil
}

func (f *fakeAdjust) Apply(_ context.Context, _ participants.Actor, _ int64, ch adjust.Change, c adjust.Confirmation) (*adjust.Result, error) {
	f.lastConf = c
	if f.applyErr != nil {
		return nil, f.applyErr
	}
	f.applied = append(f.applied, ch)
	return f.result, nil
}

type fakePeople []participants.Participant

func (f fakePeople) List(context.Context, participants.Actor, int64) ([]participants.Participant, error) {
	return f, nil
}

type fakeMembership map[int64]bool

func (f fakeMembership) InDraw(context.Context, int64) (map[int64]bool, error) { return f, nil }

type fakeAudit struct{ actions []string }

func (f *fakeAudit) Record(_ context.Context, _ participants.Actor, _ int64, action string, _ int64) error {
	f.actions = append(f.actions, action)
	return nil
}

type env struct {
	t       *testing.T
	adj     *fakeAdjust
	audit   *fakeAudit
	pages   *Pages
	sess    *websec.Sessions
	gid     int64
	handler http.Handler
	cat     *catalog.Catalog
	changed int
}

func newEnv(t *testing.T, in fakeMembership) *env {
	t.Helper()
	d, err := db.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = d.Close() })
	for _, s := range []struct {
		module string
		load   func() ([]db.Migration, error)
	}{{groups.Module, groups.Migrations}, {websec.MigrationModule, websec.Migrations}} {
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
	res, err := d.Exec(`INSERT INTO groups (slug, name, admin_secret_hash, created_at, max_gifts_per_giver)
		VALUES ('noel', 'Groupe', zeroblob(32), ?, 2)`, t0.Format(time.RFC3339Nano))
	if err != nil {
		t.Fatal(err)
	}
	gid, _ := res.LastInsertId()
	cat, err := catalog.Load(web.I18n, catalog.Options{DefaultLang: "fr", Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})
	if err != nil {
		t.Fatal(err)
	}
	e := &env{t: t, gid: gid, cat: cat, adj: &fakeAdjust{preview: &adjust.Preview{Possible: true, Affected: 1, AlreadyDiscovered: 1, Revision: 3}}, audit: &fakeAudit{}}
	e.adj.result = &adjust.Result{Preview: adjust.Preview{Possible: true, Affected: 1, AlreadyDiscovered: 1, Revision: 4}, Notified: 1}
	people := fakePeople{
		{ID: 1, FirstName: "Zxalice", Name: "Zxovitch"}, {ID: 2, FirstName: "Zxbob", Name: "Zxovitch"}, {ID: 3, FirstName: "Zxcarl", Name: "Zxovitch"},
	}
	e.pages, err = NewPages(Options{Groups: gs, Adjust: e.adj, People: people, Membership: in, Translator: cat, Audit: e.audit,
		OnDrawChanged: func(context.Context, int64) error { e.changed++; return nil }})
	if err != nil {
		t.Fatal(err)
	}
	pol, err := websec.NewPolicy(publicURL)
	if err != nil {
		t.Fatal(err)
	}
	e.sess = websec.NewSessions(d, clk, pol, websec.SessionOptions{})
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

func (e *env) cookie() *http.Cookie {
	rec := httptest.NewRecorder()
	if _, err := e.sess.Create(context.Background(), rec, nil, websec.Identity{Kind: websec.KindAdmin, GroupID: strconv.FormatInt(e.gid, 10)}); err != nil {
		e.t.Fatal(err)
	}
	for _, c := range rec.Result().Cookies() {
		if c.Name == e.sess.CookieName(websec.KindAdmin) {
			return c
		}
	}
	e.t.Fatal("cookie absent")
	return nil
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

var csrfRe = regexp.MustCompile(`name="csrf_token" value="([^"]+)"`)

func (e *env) post(path, tokenPage string, ck *http.Cookie, fields url.Values) *httptest.ResponseRecorder {
	page := e.get(tokenPage, "fr", ck)
	m := csrfRe.FindStringSubmatch(page.Body.String())
	if m == nil {
		e.t.Fatalf("jeton CSRF introuvable : %d", page.Code)
	}
	form := url.Values{websec.CSRFFieldName: {m[1]}}
	for k, v := range fields {
		form[k] = v
	}
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Sec-Fetch-Site", "same-origin")
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

func TestRoutesAndGetNeverModifies(t *testing.T) {
	e := newEnv(t, fakeMembership{1: true, 2: true})
	for _, r := range e.pages.Routes() {
		if strings.HasPrefix(r.Pattern, "POST ") != r.Modifies {
			t.Errorf("%s : Modifies incohérent", r.Pattern)
		}
		if r.Modifies {
			req := httptest.NewRequest(http.MethodGet, "/noel/admin/ajuster", nil)
			rec := httptest.NewRecorder()
			r.Handler.ServeHTTP(rec, req)
			if rec.Code != http.StatusMethodNotAllowed {
				t.Errorf("GET sur %s : %d", r.Pattern, rec.Code)
			}
		}
	}
	ck := e.cookie()
	e.get("/noel/admin/ajuster?mode=retirer&personne=1", "fr", ck)
	if len(e.adj.applied) != 0 || len(e.audit.actions) != 0 {
		t.Fatal("un GET a modifié l'état")
	}
}

func TestRequiresAdminSession(t *testing.T) {
	e := newEnv(t, fakeMembership{1: true})
	if rec := e.get("/noel/admin/ajuster", "fr", nil); rec.Code != http.StatusSeeOther {
		t.Fatalf("sans session : %d", rec.Code)
	}
}

func TestRemovePreviewListsOnlyPeopleInDrawAndShowsCountsWithoutNames(t *testing.T) {
	e := newEnv(t, fakeMembership{1: true, 2: true})
	rec := e.get("/noel/admin/ajuster?mode=retirer&personne=1", "fr", e.cookie())
	body := rec.Body.String()
	if rec.Code != 200 {
		t.Fatalf("%d", rec.Code)
	}
	if !strings.Contains(body, "Zxalice") || !strings.Contains(body, "Zxbob") || strings.Contains(body, "Zxcarl") {
		t.Fatal("liste des personnes du tirage incorrecte")
	}
	i := strings.Index(body, `id="adj-prev"`)
	if i < 0 {
		t.Fatal("aperçu absent")
	}
	if prev := body[i:]; strings.Contains(prev, "Zx") {
		t.Fatal("un nom apparaît dans l'aperçu")
	}
	if !strings.Contains(body, `name="revision" value="3"`) {
		t.Fatal("révision non reportée")
	}
	add := e.get("/noel/admin/ajuster?mode=ajouter", "fr", e.cookie()).Body.String()
	if !strings.Contains(add, "Zxcarl") || strings.Contains(add, "Zxalice") {
		t.Fatal("la liste d'ajout doit contenir seulement les personnes hors tirage")
	}
}

func TestNothingToAdjustBeforeDraw(t *testing.T) {
	e := newEnv(t, fakeMembership{})
	rec := e.get("/noel/admin/ajuster", "fr", e.cookie())
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), "rien à ajuster") {
		t.Fatalf("%d", rec.Code)
	}
}

func TestApplyRequiresConfirmationThenApplies(t *testing.T) {
	e := newEnv(t, fakeMembership{1: true, 2: true})
	ck := e.cookie()
	page := "/noel/admin/ajuster?mode=retirer&personne=1"
	rec := e.post("/noel/admin/ajuster", page, ck, url.Values{"mode": {"retirer"}, "personne": {"1"}, "revision": {"3"}})
	if rec.Code != 422 || len(e.adj.applied) != 0 {
		t.Fatalf("sans confirmation : %d", rec.Code)
	}
	rec = e.post("/noel/admin/ajuster", page, ck, url.Values{"mode": {"retirer"}, "personne": {"1"}, "revision": {"3"}, "confirm": {"1"}})
	if rec.Code != 200 || len(e.adj.applied) != 1 {
		t.Fatalf("avec confirmation : %d", rec.Code)
	}
	if c := e.adj.applied[0]; c.Kind != adjust.KindRemove || c.ParticipantID != 1 || !e.adj.lastConf.Confirmed || e.adj.lastConf.Revision != 3 {
		t.Fatalf("appel incorrect : %+v %+v", c, e.adj.lastConf)
	}
	if !strings.Contains(rec.Body.String(), `id="adj-res"`) {
		t.Fatal("résultat absent")
	}
	if len(e.audit.actions) != 1 || e.audit.actions[0] != ActionAdjust || e.changed != 1 {
		t.Fatal("journal ou rappel manquant")
	}
}

func TestRefusalProposesFullDraw(t *testing.T) {
	e := newEnv(t, fakeMembership{1: true, 2: true})
	e.adj.preview = &adjust.Preview{Possible: false, Refusal: &adjust.Refusal{Cause: drawengine.CauseTooManyOneGift, Capacity: true, Missing: 2, Message: "TEXTE-DU-SERVICE"}}
	body := e.get("/noel/admin/ajuster?mode=retirer&personne=1", "fr", e.cookie()).Body.String()
	if !strings.Contains(body, `id="adj-nofix"`) || !strings.Contains(body, `href="/noel/admin/tirage"`) || strings.Contains(body, "TEXTE-DU-SERVICE") {
		t.Fatal("proposition de tirage complet absente")
	}
	if !strings.Contains(body, "2 personnes") {
		t.Fatal("compte manquant")
	}
	// Refus au moment d'appliquer.
	e.adj.applyErr = &adjust.Refusal{Cause: drawengine.CauseTooConstrained, Message: "X"}
	rec := e.post("/noel/admin/ajuster", "/noel/admin/ajuster", e.cookie(), url.Values{"mode": {"retirer"}, "personne": {"1"}, "revision": {"3"}, "confirm": {"1"}})
	if rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), `id="adj-nofix"`) {
		t.Fatalf("refus à l'application : %d", rec.Code)
	}
	e.adj.applyErr = errs.Conflict("x")
	rec = e.post("/noel/admin/ajuster", "/noel/admin/ajuster", e.cookie(), url.Values{"mode": {"retirer"}, "personne": {"1"}, "revision": {"3"}, "confirm": {"1"}})
	if rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), `role="alert"`) {
		t.Fatalf("conflit : %d", rec.Code)
	}
}

func TestCatalogsAndEnglishRender(t *testing.T) {
	load := func(lang string) map[string]string {
		b, err := fs.ReadFile(web.I18n, "static/i18n/admin/adjust/"+lang+".json")
		if err != nil {
			t.Fatal(err)
		}
		m := map[string]string{}
		if err := json.Unmarshal(b, &m); err != nil {
			t.Fatal(err)
		}
		return m
	}
	fr, en := load("fr"), load("en")
	param := regexp.MustCompile(`\{[a-z_]+\}`)
	for k, v := range fr {
		if !strings.HasPrefix(k, "admin.adjust.") {
			t.Errorf("préfixe : %s", k)
		}
		w, ok := en[k]
		if !ok {
			t.Errorf("%s absent en anglais", k)
			continue
		}
		if strings.Join(param.FindAllString(v, -1), ",") != strings.Join(param.FindAllString(w, -1), ",") {
			t.Errorf("paramètres différents : %s", k)
		}
	}
	for k := range en {
		if _, ok := fr[k]; !ok {
			t.Errorf("%s absent en français", k)
		}
	}
	rawKey := regexp.MustCompile(`\badmin\.adjust\.[a-z_]+(\.[a-z_]+)*`)
	e := newEnv(t, fakeMembership{1: true, 2: true})
	ck := e.cookie()
	pages := []string{"/noel/admin/ajuster", "/noel/admin/ajuster?mode=ajouter", "/noel/admin/ajuster?mode=retirer&personne=1"}
	e.adj.preview = &adjust.Preview{Possible: false, Refusal: &adjust.Refusal{Cause: drawengine.CauseCapTooLow, Capacity: true, Missing: 1}}
	pages = append(pages, "/noel/admin/ajuster?mode=retirer&personne=2")
	for _, p := range pages {
		for _, lang := range []string{"en", "fr"} {
			body := e.get(p, lang, ck).Body.String()
			if m := rawKey.FindString(body); m != "" {
				t.Errorf("%s (%s) : clé brute %s", p, lang, m)
			}
			if lang == "en" {
				un := strings.NewReplacer("&#39;", "'", "&#34;", `"`).Replace(body)
				for k, v := range fr {
					if len(v) >= 8 && en[k] != v && strings.Contains(un, v) {
						t.Errorf("%s : français %q dans le rendu anglais", p, v)
					}
				}
			}
		}
	}
	e.adj.preview = &adjust.Preview{Possible: true, Affected: 1, Revision: 3}
	rec := e.post("/noel/admin/ajuster", "/noel/admin/ajuster", ck, url.Values{"mode": {"retirer"}, "personne": {"1"}, "revision": {"3"}, "confirm": {"1"}})
	if m := rawKey.FindString(rec.Body.String()); m != "" {
		t.Errorf("résultat : clé brute %s", m)
	}
}

func TestTemplatesHaveNoInlineStyleScriptOrDataURL(t *testing.T) {
	bad := regexp.MustCompile(`(?i)<style[\s>]|\sstyle\s*=|<script(\s[^>]*)?>[^<]|\son[a-z]+\s*=|data:[a-z]+/|url\(\s*["']?data:`)
	if _, err := fs.Stat(web.Static, "static/css/admin/adjust/adjust.css"); err != nil {
		if _, err2 := fs.Stat(web.Static, "css/admin/adjust/adjust.css"); err2 != nil {
			t.Fatalf("feuille de style absente : %v", err)
		}
	}
	b, err := fs.ReadFile(web.Templates, "templates/admin/adjust/adjust.html")
	if err != nil {
		t.Fatal(err)
	}
	if bad.Match(b) {
		t.Fatal("gabarit : style, script ou data: en ligne")
	}
}
