package admin

import (
	"io/fs"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"

	"secretsanta/internal/websec"
	"secretsanta/web"
)

var (
	inlineStyleRe = regexp.MustCompile(`(?is)<[a-z][^>]*\sstyle\s*=|<style\b`)
	dataURLRe     = regexp.MustCompile(`(?i)data:`)
	inlineEventRe = regexp.MustCompile(`(?is)<[a-z][^>]*\son[a-z]+\s*=`)
)

var modifying = []struct {
	name    string
	path    string
	handler func(*Pages) http.Handler
}{
	{"mode", "/famille/admin/inscriptions/mode", (*Pages).Mode},
	{"renouveler", "/famille/admin/inscriptions/renouveler", (*Pages).Renew},
	{"limites", "/famille/admin/inscriptions/limites", (*Pages).Limits},
	{"valider", "/famille/admin/inscriptions/7/valider", (*Pages).Validate},
	{"renvoyer", "/famille/admin/inscriptions/7/renvoyer", (*Pages).Resend},
	{"supprimer", "/famille/admin/inscriptions/7/supprimer", (*Pages).Delete},
}

// F11c.AC13 : un GET sur une route modifiante renvoie 405 sans rien modifier.
func TestModifyingHandlersRefuseSafeMethods(t *testing.T) {
	e := newEnv(t)
	for _, m := range modifying {
		for _, method := range []string{http.MethodGet, http.MethodHead, http.MethodOptions, http.MethodTrace} {
			w := httptest.NewRecorder()
			r := httptest.NewRequest(method, m.path, nil)
			r.SetPathValue("slug", "famille")
			r.SetPathValue("id", "7")
			for _, c := range e.cookies["famille"] {
				r.AddCookie(c)
			}
			m.handler(e.pages).ServeHTTP(w, r)
			if w.Code != http.StatusMethodNotAllowed {
				t.Errorf("%s %s : %d, 405 attendu", method, m.name, w.Code)
			}
		}
		// Sur la chaîne réelle (le mux ne route pas le GET : 405 aussi).
		if w := e.do("GET", m.path, "", nil, e.cookies["famille"]); w.Code != http.StatusMethodNotAllowed {
			t.Errorf("chaîne, GET %s : %d", m.name, w.Code)
		}
	}
	if e.inv.callCount() != 0 || e.ver.count() != 0 {
		t.Errorf("un GET a modifié l'état : %v %v", e.inv.calls, e.ver.calls)
	}
}

// Un POST sans jeton CSRF est refusé et ne modifie rien.
func TestModifyingHandlersNeedCSRF(t *testing.T) {
	e := newEnv(t)
	for _, m := range modifying {
		if w := e.do("POST", m.path, "", url.Values{"enabled": {"1"}}, e.cookies["famille"]); w.Code != http.StatusForbidden {
			t.Errorf("%s sans jeton : %d, 403 attendu", m.name, w.Code)
		}
	}
	if e.inv.callCount() != 0 || e.ver.count() != 0 {
		t.Error("une requête sans jeton a modifié l'état")
	}
}

func TestTemplatesRespectStrictCSP(t *testing.T) {
	n := 0
	err := fs.WalkDir(web.Templates, "templates/registration/admin", func(p string, d fs.DirEntry, err error) error {
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

func TestRenderedPageRespectsStrictCSP(t *testing.T) {
	e := newEnv(t)
	e.post(t, "famille", "/famille/admin/inscriptions/mode", url.Values{"enabled": {"1"}})
	body := e.do("GET", "/famille/admin/inscriptions?confirm=renew", "en", nil, e.cookies["famille"]).Body.String()
	if !strings.Contains(body, "regadm") {
		t.Fatal("page non rendue")
	}
	if inlineStyleRe.MatchString(body) || dataURLRe.MatchString(body) || inlineEventRe.MatchString(body) {
		t.Error("style, data: ou on* en ligne dans le rendu")
	}
	for _, problem := range websec.CheckTemplate("rendu", body) {
		t.Error(problem)
	}
}
