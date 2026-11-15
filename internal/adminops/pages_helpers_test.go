package adminops_test

import (
	"bytes"
	"context"
	"encoding/json"
	"html/template"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"secretsanta/internal/adminops"
	"secretsanta/internal/i18n/catalog"
	"secretsanta/internal/i18n/negotiate"
	i18nweb "secretsanta/internal/i18n/web"
	"secretsanta/internal/shared/adminnav"
	"secretsanta/internal/websec"
	"secretsanta/web"
)

const testOrigin = "http://127.0.0.1:8080"

type webEnv struct {
	*fixture
	pages   *adminops.Pages
	handler http.Handler
	cookies []*http.Cookie
	gid     int64
	secret  string
}

func realRenderer(t *testing.T, cat *catalog.Catalog) adminops.Renderer {
	t.Helper()
	tm := template.New("root").Funcs(websec.TemplateFuncsPlaceholder()).Funcs(i18nweb.FuncsPlaceholder())
	tm, err := tm.ParseFS(web.Templates, "templates/layout/*.html", "templates/i18n/*.html", "templates/adminops/*.html")
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

func newWebEnv(t *testing.T) *webEnv {
	t.Helper()
	f := newFixture(t)
	gid, secret := f.group(t, "famille", "Famille", "2026-12-24")
	f.person(t, gid, "Anne", "Leroy", "anne@exemple.fr", "")
	pol, err := websec.NewPolicy(testOrigin)
	if err != nil {
		t.Fatal(err)
	}
	sess := websec.NewSessions(f.db, f.clock, pol, websec.SessionOptions{})
	cat, err := catalog.Load(web.I18n, catalog.Options{DefaultLang: "fr", Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})
	if err != nil {
		t.Fatal(err)
	}
	reg := adminnav.New()
	reg.Declare(adminnav.Entry{Key: "plus", Label: "layout.nav.more", Icon: "more", Rank: adminnav.RankMore, Destination: "/{slug}/admin/plus"})
	p, err := adminops.New(adminops.Deps{Groups: f.groups, Ops: f.ops, Render: realRenderer(t, cat), Nav: reg,
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	p.Register(mux)
	e := &webEnv{fixture: f, pages: p, gid: gid, secret: secret}
	e.handler = websec.NewCSRF(pol, websec.CSRFOptions{}).Middleware(sess.Middleware(mux))
	w := httptest.NewRecorder()
	if _, err := sess.Create(context.Background(), w, httptest.NewRequest("GET", "/", nil),
		websec.Identity{Kind: websec.KindAdmin, GroupID: strconv.FormatInt(gid, 10)}); err != nil {
		t.Fatal(err)
	}
	e.cookies = w.Result().Cookies()
	return e
}

func (e *webEnv) request(method, target, lang, contentType string, body io.Reader, cookies []*http.Cookie) *httptest.ResponseRecorder {
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

func (e *webEnv) get(target, lang string) *httptest.ResponseRecorder {
	return e.request("GET", target, lang, "", nil, e.cookies)
}

var csrfRe = regexp.MustCompile(`name="csrf_token" value="([^"]+)"`)

// csrf lit le jeton de la page « Données » et renvoie les cookies à joindre.
func (e *webEnv) csrf(t *testing.T) (string, []*http.Cookie) {
	t.Helper()
	page := e.get("/famille/admin/donnees", "")
	m := csrfRe.FindStringSubmatch(page.Body.String())
	if m == nil {
		t.Fatalf("jeton CSRF absent (%d)", page.Code)
	}
	cookies := append(append([]*http.Cookie{}, e.cookies...), page.Result().Cookies()...)
	return strings.ReplaceAll(m[1], "&#43;", "+"), cookies
}

func (e *webEnv) postForm(t *testing.T, target, lang string, form url.Values) *httptest.ResponseRecorder {
	t.Helper()
	tok, cookies := e.csrf(t)
	form.Set("csrf_token", tok)
	return e.request("POST", target, lang, "application/x-www-form-urlencoded", strings.NewReader(form.Encode()), cookies)
}

func (e *webEnv) postFile(t *testing.T, target, lang string, content []byte) *httptest.ResponseRecorder {
	t.Helper()
	tok, cookies := e.csrf(t)
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	_ = mw.WriteField("csrf_token", tok)
	if content != nil {
		fw, err := mw.CreateFormFile("file", "participants.json")
		if err != nil {
			t.Fatal(err)
		}
		_, _ = fw.Write(content)
	}
	_ = mw.Close()
	return e.request("POST", target, lang, mw.FormDataContentType(), &body, cookies)
}

func catalogFile(t *testing.T, lang string) map[string]string {
	t.Helper()
	raw, err := web.I18n.ReadFile("static/i18n/adminops/" + lang + ".json")
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]string{}
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatal(err)
	}
	return out
}
