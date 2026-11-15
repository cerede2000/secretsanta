package mailunsub

import (
	"io/fs"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"

	"secretsanta/internal/websec"
	"secretsanta/web"
)

var (
	inlineStyleRe = regexp.MustCompile(`(?i)<style[\s>]|\sstyle\s*=`)
	inlineScript  = regexp.MustCompile(`(?i)<script(\s[^>]*)?>[^<]|\son[a-z]+\s*=`)
	dataURLRe     = regexp.MustCompile(`(?i)data:[a-z]+/|url\(\s*["']?data:`)
)

// F6.AC35 : un GET sur une route modifiante renvoie 405 sans rien modifier.
func TestStateChangingRoutesRejectGet(t *testing.T) {
	e := newEnv(t)
	path := e.link(t, TypeRelances)
	n := 0
	for _, r := range e.pages.Routes() {
		if !r.Modifies {
			continue
		}
		n++
		for _, m := range []string{http.MethodGet, http.MethodHead, http.MethodOptions} {
			req := httptest.NewRequest(m, path, nil)
			req.SetPathValue("token", strings.TrimPrefix(path, RoutePrefix))
			rec := httptest.NewRecorder()
			r.Handler.ServeHTTP(rec, req)
			if rec.Code != http.StatusMethodNotAllowed {
				t.Errorf("%s %s : 405 attendu, %d", m, r.Pattern, rec.Code)
			}
		}
	}
	if n != 1 {
		t.Fatalf("routes modifiantes : %d", n)
	}
	if off(t, e, TypeRelances) {
		t.Fatal("un GET a modifié l'état")
	}
	for _, r := range e.pages.Routes() {
		if strings.HasPrefix(r.Pattern, "POST ") != r.Modifies {
			t.Errorf("%s : Modifies=%v incohérent", r.Pattern, r.Modifies)
		}
	}
}

// Aucune exemption CSRF au-delà du POST signé de désinscription.
func TestCSRFExemptionIsOnlyThePostOfTheUnsubscribeRoute(t *testing.T) {
	for _, c := range []struct {
		method, path string
		want         bool
	}{
		{"POST", "/desinscription/abc.def", true},
		{"POST", "/noel/connexion/code", false},
		{"POST", "/desinscription/", false},
		{"GET", "/desinscription/abc.def", false},
	} {
		req := httptest.NewRequest(c.method, c.path, nil)
		if IsOneClickPath(req) != c.want {
			t.Errorf("%s %s : %v attendu", c.method, c.path, c.want)
		}
	}
}

func TestTemplatesAndAssetsHaveNoInlineStyleScriptOrDataURL(t *testing.T) {
	count := 0
	for _, f := range []struct {
		fsys fs.FS
		name string
	}{
		{web.Templates, "templates/mail/unsubscribe.html"},
		{web.Static, "static/css/mail/unsubscribe.css"},
	} {
		b, err := fs.ReadFile(f.fsys, f.name)
		if err != nil {
			t.Fatal(err)
		}
		src := string(b)
		if inlineStyleRe.MatchString(src) || inlineScript.MatchString(src) || dataURLRe.MatchString(src) {
			t.Errorf("%s : contenu interdit par la CSP", f.name)
		}
		for _, p := range websec.CheckTemplate(f.name, src) {
			t.Errorf("%s : %s", f.name, p)
		}
		count++
	}
	if count != 2 {
		t.Fatal("fichiers non vérifiés")
	}
}

func TestRenderedPageHasNoInlineStyleOrDataURL(t *testing.T) {
	e := newEnv(t)
	for _, typ := range []string{TypeRelances, TypeNouvelleIdee} {
		rec := e.do(http.MethodGet, e.link(t, typ), nil, "fr")
		body := rec.Body.String()
		if rec.Code != 200 || inlineStyleRe.MatchString(body) || dataURLRe.MatchString(body) || strings.Contains(body, "<script>") {
			t.Errorf("%s : %d ou contenu interdit", typ, rec.Code)
		}
		if !strings.Contains(body, "/static/css/mail/unsubscribe.css") {
			t.Error("feuille de style absente")
		}
	}
}
