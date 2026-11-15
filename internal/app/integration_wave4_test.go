package app

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"
)

var csrfRe = regexp.MustCompile(`name="csrf_token" value="([^"]+)"`)

// postLang récupère un jeton CSRF via GET / puis poste le changement de langue.
func postLang(t *testing.T, a *App, lang, next string) *httptest.ResponseRecorder {
	t.Helper()
	rec := get(t, a, "/")
	m := csrfRe.FindStringSubmatch(rec.Body.String())
	if m == nil {
		t.Fatalf("jeton CSRF absent : %s", rec.Body.String())
	}
	form := url.Values{"csrf_token": {m[1]}, "lang": {lang}, "next": {next}}
	req := httptest.NewRequest(http.MethodPost, "/langue", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Sec-Fetch-Site", "same-origin")
	for _, c := range rec.Result().Cookies() {
		req.AddCookie(c)
	}
	out := httptest.NewRecorder()
	a.Handler().ServeHTTP(out, req)
	return out
}

func TestHomeEnglishWithSelector(t *testing.T) {
	a := newApp(t)
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Cookie", "lang=en")
	rec := httptest.NewRecorder()
	a.Handler().ServeHTTP(rec, req)
	body := rec.Body.String()
	if !strings.Contains(body, `<html lang="en">`) {
		t.Errorf("langue : %s", body)
	}
	i := strings.Index(body, "<footer")
	if i < 0 || !strings.Contains(body[i:], `class="lang-selector"`) {
		t.Errorf("sélecteur absent du pied de page")
	}
}

func TestLangueSetsCookieAndRedirects(t *testing.T) {
	a := newApp(t)
	rec := postLang(t, a, "en", "/_skeleton/admin?x=1")
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("statut %d", rec.Code)
	}
	if loc := rec.Header().Get("Location"); loc != "/_skeleton/admin?x=1" {
		t.Errorf("Location %q", loc)
	}
	var found bool
	for _, c := range rec.Result().Cookies() {
		if c.Name == "lang" {
			found = true
			if c.Value != "en" || !c.HttpOnly || c.SameSite != http.SameSiteLaxMode || c.MaxAge != 365*24*3600 {
				t.Errorf("cookie %+v", c)
			}
		}
	}
	if !found {
		t.Error("cookie lang absent")
	}
}

func TestLangueRefusesExternalNext(t *testing.T) {
	a := newApp(t)
	for _, next := range []string{"https://evil.example", "//evil.example"} {
		rec := postLang(t, a, "en", next)
		if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/" {
			t.Errorf("%q : %d %q", next, rec.Code, rec.Header().Get("Location"))
		}
	}
}

func TestLangueWithoutCSRFRefused(t *testing.T) {
	a := newApp(t)
	req := httptest.NewRequest(http.MethodPost, "/langue", strings.NewReader("lang=en"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Sec-Fetch-Site", "same-origin")
	rec := httptest.NewRecorder()
	a.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Errorf("statut %d", rec.Code)
	}
}

func TestServiceWorkerHeaders(t *testing.T) {
	a := newApp(t)
	rec := get(t, a, "/sw.js")
	if rec.Code != http.StatusOK {
		t.Fatalf("statut %d", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "text/javascript" {
		t.Errorf("Content-Type %q", ct)
	}
	if h := rec.Header().Get("Service-Worker-Allowed"); h != "/" {
		t.Errorf("Service-Worker-Allowed %q", h)
	}
}

func TestStaticTypes(t *testing.T) {
	a := newApp(t)
	for path, want := range map[string]string{
		"/static/manifest.webmanifest":                "application/manifest+json",
		"/static/fonts/nunito-latin-400-normal.woff2": "font/woff2",
		"/static/css/theme/icons/icon-192.png":        "image/png",
		"/static/css/theme/icons/icon.svg":            "image/svg+xml",
		"/static/js/theme/snow.js":                    "text/javascript",
		"/static/css/theme/theme.css":                 "text/css",
	} {
		rec := get(t, a, path)
		if rec.Code != http.StatusOK {
			t.Errorf("%s : statut %d", path, rec.Code)
			continue
		}
		if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, want) {
			t.Errorf("%s : Content-Type %q, attendu %q", path, ct, want)
		}
	}
}

func TestImagesUnknown404(t *testing.T) {
	a := newApp(t)
	if rec := get(t, a, "/images/inconnu.jpg"); rec.Code != http.StatusNotFound {
		t.Errorf("statut %d", rec.Code)
	}
	req := httptest.NewRequest(http.MethodHead, "/images/inconnu.jpg", nil)
	rec := httptest.NewRecorder()
	a.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Errorf("HEAD statut %d", rec.Code)
	}
}

func TestWave4Services(t *testing.T) {
	a := newApp(t)
	if a.Letter == nil || a.SSRF == nil || a.Images == nil || a.Bus == nil {
		t.Fatalf("service manquant : %+v", a)
	}
}

func TestHomeThemeAndNoInline(t *testing.T) {
	body := get(t, newApp(t), "/").Body.String()
	for _, want := range []string{"/static/css/theme/theme.css", "/static/manifest.webmanifest", `name="theme-color"`, "apple-touch-icon.png", "/static/js/theme/snow.js", "/static/js/avatars/avatars.js"} {
		if !strings.Contains(body, want) {
			t.Errorf("absent : %s", want)
		}
	}
	if strings.Count(body, "<script") != strings.Count(body, `<script type="module" src="/static/js/`) || strings.Contains(body, "></script>\n<script>") {
		t.Error("script en ligne")
	}
	for _, banned := range []string{"style=", "<style", " onclick=", "http://", "https://"} {
		if strings.Contains(body, banned) {
			t.Errorf("contient %q", banned)
		}
	}
}
