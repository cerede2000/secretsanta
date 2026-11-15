package maildiagpages_test

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"html/template"
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

	"secretsanta/internal/app"
	"secretsanta/internal/config"
	"secretsanta/internal/db"
	"secretsanta/internal/groups"
	"secretsanta/internal/i18n/catalog"
	"secretsanta/internal/i18n/negotiate"
	i18nweb "secretsanta/internal/i18n/web"
	"secretsanta/internal/mail/queue"
	"secretsanta/internal/maildiag"
	"secretsanta/internal/maildiagpages"
	"secretsanta/internal/mailmsg"
	"secretsanta/internal/mailsuppress"
	"secretsanta/internal/shared/adminnav"
	"secretsanta/internal/shared/clock"
	"secretsanta/internal/websec"
	"secretsanta/web"
)

const (
	instancePass = "mot-de-passe-instance-de-test"
	testOrigin   = "http://127.0.0.1:8080"
)

var t0 = time.Date(2026, 11, 15, 10, 0, 0, 0, time.UTC)

type fakeDiag struct{ rep maildiag.Report }

func (f fakeDiag) Run(context.Context) maildiag.Report { return f.rep }

// fakeMailer simule le service de test de la file.
type fakeMailer struct {
	items []queue.Item
	st    queue.Status
	err   error
}

func (f *fakeMailer) SendTest(_ context.Context, it queue.Item) (queue.Status, error) {
	f.items = append(f.items, it)
	st := f.st
	st.To = it.To
	return st, f.err
}

type fakeBuilder struct{}

func (fakeBuilder) Build(m mailmsg.Message) (*mailmsg.Built, error) {
	raw := "From: santa@exemple.fr\r\nTo: " + m.To + "\r\nSubject: " + m.Subject + "\r\nMessage-ID: <abc@exemple.fr>\r\n\r\nCorps secret\r\n"
	return &mailmsg.Built{To: m.To, Raw: []byte(raw)}, nil
}

func defaultReport() maildiag.Report {
	return maildiag.Report{
		Domain: "exemple.fr",
		Checks: []maildiag.Check{
			{ID: maildiag.CheckSPF, Status: maildiag.StatusOK, Code: "spf.hardfail", Params: map[string]string{"domain": "exemple.fr"}},
			{ID: maildiag.CheckDMARC, Status: maildiag.StatusWarn, Code: "dmarc.missing", Params: map[string]string{"domain": "exemple.fr"},
				Record: &maildiag.Record{Name: "_dmarc.exemple.fr", Type: "TXT", Value: `"v=DMARC1; p=none"`}},
			{ID: maildiag.CheckDKIM, Status: maildiag.StatusOff, Code: "dkim.off"},
		},
		NotVerifiable: maildiag.NotVerifiableItems(),
	}
}

type env struct {
	db      *sql.DB
	groups  *groups.Service
	supp    *mailsuppress.Store
	mailer  *fakeMailer
	diag    *fakeDiag
	handler http.Handler
	cookies []*http.Cookie
	gid     int64
	gid2    int64
}

func newEnv(t *testing.T) *env {
	t.Helper()
	d, err := db.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = d.Close() })
	if err := app.Migrate(d); err != nil {
		t.Fatal(err)
	}
	clk := clock.NewFake(t0)
	gs, err := groups.NewService(d, clk, config.NewSecret(instancePass))
	if err != nil {
		t.Fatal(err)
	}
	mk := func(slug, name string) int64 {
		res, err := gs.Create(context.Background(), groups.CreateInput{InstancePassword: instancePass, ClientKey: "test",
			Name: name, Slug: slug, Budget: "25", GiftInstructions: "Emballé", EventDate: "2026-12-24"})
		if err != nil {
			t.Fatal(err)
		}
		return res.Group.ID
	}
	e := &env{db: d, groups: gs, supp: mailsuppress.New(d, clk), mailer: &fakeMailer{st: queue.Status{State: queue.StateSent, SentAt: t0}},
		diag: &fakeDiag{rep: defaultReport()}}
	e.gid, e.gid2 = mk("famille", "Famille"), mk("amis", "Amis")

	pol, err := websec.NewPolicy(testOrigin)
	if err != nil {
		t.Fatal(err)
	}
	sess := websec.NewSessions(d, clk, pol, websec.SessionOptions{})
	cat, err := catalog.Load(web.I18n, catalog.Options{DefaultLang: "fr", Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})
	if err != nil {
		t.Fatal(err)
	}
	reg := adminnav.New()
	reg.Declare(adminnav.Entry{Key: "plus", Label: "layout.nav.more", Icon: "more", Rank: adminnav.RankMore, Destination: "/{slug}/admin/plus"})
	p, err := maildiagpages.New(maildiagpages.Deps{Groups: gs, Diagnostic: e.diag, Suppressions: e.supp, Mailer: e.mailer,
		Builder: fakeBuilder{}, Catalog: cat, Render: realRenderer(t, cat), Nav: reg, Clock: clk,
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	p.Register(mux)
	e.handler = websec.NewCSRF(pol, websec.CSRFOptions{}).Middleware(sess.Middleware(mux))
	w := httptest.NewRecorder()
	if _, err := sess.Create(context.Background(), w, httptest.NewRequest("GET", "/", nil),
		websec.Identity{Kind: websec.KindAdmin, GroupID: strconv.FormatInt(e.gid, 10)}); err != nil {
		t.Fatal(err)
	}
	e.cookies = w.Result().Cookies()
	return e
}

func realRenderer(t *testing.T, cat *catalog.Catalog) maildiagpages.Renderer {
	t.Helper()
	tm := template.New("root").Funcs(websec.TemplateFuncsPlaceholder()).Funcs(i18nweb.FuncsPlaceholder())
	tm, err := tm.ParseFS(web.Templates, "templates/layout/*.html", "templates/i18n/*.html", "templates/maildiag/*.html")
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

func (e *env) request(method, target, lang, contentType string, body io.Reader, cookies []*http.Cookie) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, target, body)
	if contentType != "" {
		r.Header.Set("Content-Type", contentType)
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

func (e *env) get(target, lang string) *httptest.ResponseRecorder {
	return e.request("GET", target, lang, "", nil, e.cookies)
}

var csrfRe = regexp.MustCompile(`name="csrf_token" value="([^"]+)"`)

func (e *env) csrf(t *testing.T) (string, []*http.Cookie) {
	t.Helper()
	page := e.get("/famille/admin/courrier/delivrabilite", "")
	m := csrfRe.FindStringSubmatch(page.Body.String())
	if m == nil {
		t.Fatalf("jeton CSRF absent (%d)", page.Code)
	}
	return strings.ReplaceAll(m[1], "&#43;", "+"), append(append([]*http.Cookie{}, e.cookies...), page.Result().Cookies()...)
}

func (e *env) post(t *testing.T, target, lang string, form url.Values) *httptest.ResponseRecorder {
	t.Helper()
	tok, cookies := e.csrf(t)
	form.Set("csrf_token", tok)
	return e.request("POST", target, lang, "application/x-www-form-urlencoded", strings.NewReader(form.Encode()), cookies)
}

func catalogFile(t *testing.T, lang string) map[string]string {
	t.Helper()
	raw, err := web.I18n.ReadFile("static/i18n/maildiag/" + lang + ".json")
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]string{}
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatal(err)
	}
	return out
}

var errBoom = errors.New("boom")
