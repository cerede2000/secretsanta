package menu

import (
	"io/fs"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"testing"

	"secretsanta/internal/websec"
	"secretsanta/web"
)

var (
	inlineStyleRe = regexp.MustCompile(`(?is)<[a-z][^>]*\sstyle\s*=|<style\b`)
	dataURLRe     = regexp.MustCompile(`(?i)data:`)
	inlineEventRe = regexp.MustCompile(`(?is)<[a-z][^>]*\son[a-z]+\s*=`)
)

// F10.AC34 : un GET sur la déconnexion renvoie 405 sans rien modifier.
func TestLogoutRefusesSafeMethods(t *testing.T) {
	e := newEnv(t)
	for _, m := range []string{http.MethodGet, http.MethodHead, http.MethodOptions, http.MethodTrace} {
		w := httptest.NewRecorder()
		r := httptest.NewRequest(m, "/famille/admin/deconnexion", nil)
		for _, c := range e.cookies["famille"] {
			r.AddCookie(c)
		}
		e.pages.Logout().ServeHTTP(w, r)
		if w.Code != http.StatusMethodNotAllowed {
			t.Errorf("%s : %d, 405 attendu", m, w.Code)
		}
	}
	// Sur la chaîne réelle : le GET ne ferme pas la session.
	if w := e.do("GET", "/famille/admin/deconnexion", "", nil, e.cookies["famille"]); w.Code != 405 {
		t.Errorf("GET : %d", w.Code)
	}
	if w := e.do("GET", "/famille/admin/plus", "", nil, e.cookies["famille"]); w.Code != 200 {
		t.Errorf("la session a été fermée par un GET : %d", w.Code)
	}
}

// Un POST sans jeton CSRF est refusé ; avec le jeton, la session se ferme.
func TestLogoutNeedsCSRFAndDestroysSession(t *testing.T) {
	e := newEnv(t)
	if w := e.do("POST", "/famille/admin/deconnexion", "", url.Values{}, e.cookies["famille"]); w.Code != http.StatusForbidden {
		t.Errorf("sans jeton : %d, 403 attendu", w.Code)
	}
	page := e.do("GET", "/famille/admin/plus", "", nil, e.cookies["famille"])
	m := regexp.MustCompile(`name="csrf_token" value="([^"]+)"`).FindStringSubmatch(page.Body.String())
	if m == nil {
		t.Fatal("jeton absent")
	}
	cookies := append(append([]*http.Cookie{}, e.cookies["famille"]...), page.Result().Cookies()...)
	tok := regexp.MustCompile(`&#43;`).ReplaceAllString(m[1], "+")
	w := e.do("POST", "/famille/admin/deconnexion", "", url.Values{"csrf_token": {tok}}, cookies)
	if w.Code != 303 || w.Header().Get("Location") != "/famille/admin/connexion" {
		t.Fatalf("déconnexion : %d %q", w.Code, w.Header().Get("Location"))
	}
	if w := e.do("GET", "/famille/admin/plus", "", nil, e.cookies["famille"]); w.Code != 303 {
		t.Errorf("la session est restée valide : %d", w.Code)
	}
}

func TestTemplatesRespectStrictCSP(t *testing.T) {
	n := 0
	err := fs.WalkDir(web.Templates, "templates/admin/menu", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		n++
		b, err := fs.ReadFile(web.Templates, p)
		if err != nil {
			return err
		}
		src := string(b)
		if inlineStyleRe.MatchString(src) {
			t.Errorf("%s : style en ligne", p)
		}
		if dataURLRe.MatchString(src) {
			t.Errorf("%s : ressource data:", p)
		}
		if inlineEventRe.MatchString(src) {
			t.Errorf("%s : gestionnaire on* en ligne", p)
		}
		for _, problem := range websec.CheckTemplate(p, src) {
			t.Error(problem)
		}
		return nil
	})
	if err != nil || n == 0 {
		t.Fatalf("gabarits introuvables (%d) : %v", n, err)
	}
}

func TestRenderedMenuRespectsStrictCSP(t *testing.T) {
	e := newEnv(t)
	body := e.do("GET", "/famille/admin/plus", "en", nil, e.cookies["famille"]).Body.String()
	if inlineStyleRe.MatchString(body) || dataURLRe.MatchString(body) || inlineEventRe.MatchString(body) {
		t.Error("style, data: ou on* en ligne dans le rendu")
	}
	for _, problem := range websec.CheckTemplate("rendu", body) {
		t.Error(problem)
	}
}
