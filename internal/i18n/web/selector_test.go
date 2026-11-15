package web

import (
	"context"
	"errors"
	"html/template"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"

	"secretsanta/internal/i18n/negotiate"
	"secretsanta/internal/websec"
)

const publicURL = "https://noel.example.org"

type recorder struct {
	langs []string
	err   error
}

func (p *recorder) RememberLocale(_ context.Context, _ *http.Request, lang string) (bool, error) {
	p.langs = append(p.langs, lang)
	return true, p.err
}

type app struct {
	h   http.Handler
	rec *recorder
}

func newApp(t *testing.T, rec *recorder) *app {
	t.Helper()
	cat := testCatalog(t)
	pol, err := websec.NewPolicy(publicURL)
	if err != nil {
		t.Fatal(err)
	}
	sel := &Selector{Cat: cat, Secure: true, Participants: rec}
	tm := pageTemplate(t)
	page := template.Must(template.Must(tm.Clone()).Parse(`{{define "page"}}{{template "layout/footer" .}}{{end}}`))
	mux := http.NewServeMux()
	mux.HandleFunc("GET /page", func(w http.ResponseWriter, r *http.Request) {
		c := template.Must(page.Clone()).Funcs(websec.TemplateFuncs(r)).Funcs(Funcs(cat, r))
		if err := c.ExecuteTemplate(w, "page", map[string]any{}); err != nil {
			http.Error(w, err.Error(), 500)
		}
	})
	mux.Handle("POST "+Route, sel.Handler())
	mux.Handle("GET "+Route, sel.Handler())
	chain := websec.NewCSRF(pol, websec.CSRFOptions{}).Middleware(negotiate.Middleware(cat, "fr", nil)(mux))
	return &app{h: chain, rec: rec}
}

var (
	fieldRe = regexp.MustCompile(`name="csrf_token" value="([^"]+)"`)
	nextRe  = regexp.MustCompile(`name="next" value="([^"]*)"`)
)

func (a *app) page(t *testing.T, path string, hdr map[string]string) (*httptest.ResponseRecorder, *http.Cookie, string) {
	t.Helper()
	r := httptest.NewRequest("GET", publicURL+path, nil)
	for k, v := range hdr {
		r.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	a.h.ServeHTTP(rec, r)
	if rec.Code != 200 {
		t.Fatalf("GET %s : %d %s", path, rec.Code, rec.Body)
	}
	var ck *http.Cookie
	for _, c := range rec.Result().Cookies() {
		if c.Name == "__Host-ss_csrf" {
			ck = c
		}
	}
	m := fieldRe.FindStringSubmatch(rec.Body.String())
	if ck == nil || m == nil {
		t.Fatalf("jeton absent : %s", rec.Body)
	}
	return rec, ck, m[1]
}

func (a *app) post(form url.Values, ck *http.Cookie, sfs string) *httptest.ResponseRecorder {
	r := httptest.NewRequest("POST", publicURL+Route, strings.NewReader(form.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if sfs != "" {
		r.Header.Set("Sec-Fetch-Site", sfs)
	}
	if ck != nil {
		r.AddCookie(&http.Cookie{Name: ck.Name, Value: ck.Value})
	}
	rec := httptest.NewRecorder()
	a.h.ServeHTTP(rec, r)
	return rec
}

func TestSelectorListsLoadedLanguagesByNativeName(t *testing.T) {
	a := newApp(t, &recorder{})
	rec, _, _ := a.page(t, "/page", nil)
	body := rec.Body.String()
	for _, want := range []string{`>Français</option>`, `>English</option>`, `>Deutsch</option>`, `value="de"`, `Langue`} {
		if !strings.Contains(body, want) {
			t.Fatalf("%q absent : %s", want, body)
		}
	}
	if !strings.Contains(body, `value="fr" lang="fr" selected`) {
		t.Fatalf("langue courante non sélectionnée : %s", body)
	}
	for _, bad := range []string{"style=", "<style", "onchange=", "<script"} {
		if strings.Contains(body, bad) {
			t.Fatalf("CSP : %q dans le sélecteur", bad)
		}
	}
	rec, _, _ = a.page(t, "/page?x=1", map[string]string{"Accept-Language": "en"})
	if !strings.Contains(rec.Body.String(), `value="en" lang="en" selected`) || !strings.Contains(rec.Body.String(), "Language") {
		t.Fatalf("anglais : %s", rec.Body)
	}
	if m := nextRe.FindStringSubmatch(rec.Body.String()); m == nil || m[1] != "/page?x=1" {
		t.Fatalf("next : %v", m)
	}
}

func TestChangeNeedsCSRF(t *testing.T) {
	rec := &recorder{}
	a := newApp(t, rec)
	_, ck, tok := a.page(t, "/page", nil)
	form := url.Values{"lang": {"en"}, "next": {"/page"}}

	// Sans jeton, ou avec un mauvais jeton, ou sans cookie : refus, rien posé.
	for label, r := range map[string]*httptest.ResponseRecorder{
		"sans jeton":  a.post(form, ck, "same-origin"),
		"mauvais":     a.post(url.Values{"lang": {"en"}, "csrf_token": {"AAAA"}}, ck, "same-origin"),
		"sans cookie": a.post(url.Values{"lang": {"en"}, "csrf_token": {tok}}, nil, "same-origin"),
		"autre site":  a.post(url.Values{"lang": {"en"}, "csrf_token": {tok}}, ck, "cross-site"),
	} {
		if r.Code != http.StatusForbidden {
			t.Errorf("%s : %d", label, r.Code)
		}
		for _, c := range r.Result().Cookies() {
			if c.Name == "lang" {
				t.Errorf("%s : cookie lang posé", label)
			}
		}
	}
	if len(rec.langs) != 0 {
		t.Fatal("langue du participant enregistrée sans CSRF")
	}

	form.Set("csrf_token", tok)
	r := a.post(form, ck, "same-origin")
	if r.Code != http.StatusSeeOther || r.Header().Get("Location") != "/page" {
		t.Fatalf("%d %q", r.Code, r.Header().Get("Location"))
	}
	var lang *http.Cookie
	for _, c := range r.Result().Cookies() {
		if c.Name == "lang" {
			lang = c
		}
	}
	if lang == nil || lang.Value != "en" || lang.Path != "/" || !lang.Secure || !lang.HttpOnly || lang.SameSite != http.SameSiteLaxMode || lang.MaxAge <= 0 || lang.Domain != "" {
		t.Fatalf("cookie lang : %+v", lang)
	}
	if len(rec.langs) != 1 || rec.langs[0] != "en" {
		t.Fatalf("port ParticipantLocale : %v", rec.langs)
	}

	// Un GET ne change rien.
	g := httptest.NewRecorder()
	a.h.ServeHTTP(g, httptest.NewRequest("GET", publicURL+Route+"?lang=en", nil))
	if g.Code != http.StatusMethodNotAllowed {
		t.Fatalf("GET : %d", g.Code)
	}
}

func TestChangeValidation(t *testing.T) {
	rec := &recorder{}
	a := newApp(t, rec)
	_, ck, tok := a.page(t, "/page", nil)
	r := a.post(url.Values{"lang": {"xx"}, "csrf_token": {tok}}, ck, "same-origin")
	if r.Code != http.StatusBadRequest || len(rec.langs) != 0 {
		t.Fatalf("langue inconnue : %d", r.Code)
	}
	for _, next := range []string{"https://evil.example/", "//evil.example", "/\\evil", "javascript:alert(1)", "", "/a\r\nSet-Cookie: x=1"} {
		r := a.post(url.Values{"lang": {"fr"}, "next": {next}, "csrf_token": {tok}}, ck, "same-origin")
		if loc := r.Header().Get("Location"); loc != "/" {
			t.Errorf("next %q : redirection vers %q", next, loc)
		}
	}
}

func TestParticipantPortFailureDoesNotBlock(t *testing.T) {
	rec := &recorder{err: errors.New("base indisponible")}
	a := newApp(t, rec)
	_, ck, tok := a.page(t, "/page", nil)
	r := a.post(url.Values{"lang": {"en"}, "csrf_token": {tok}}, ck, "same-origin")
	if r.Code != http.StatusSeeOther || len(rec.langs) != 1 {
		t.Fatalf("%d %v", r.Code, rec.langs)
	}
}
