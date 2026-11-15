package public

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

// F11c.AC11 : un GET sur une route modifiante renvoie 405 sans rien modifier.
func TestStateChangingRoutesRejectGet(t *testing.T) {
	e := newEnv(t)
	n := 0
	for _, r := range e.pages.Routes() {
		if !r.Modifies {
			continue
		}
		n++
		path := strings.NewReplacer("{slug}", "noel", "{token}", "lien").Replace(strings.TrimPrefix(r.Pattern, "POST "))
		for _, m := range []string{http.MethodGet, http.MethodHead, http.MethodOptions} {
			req := httptest.NewRequest(m, path, nil)
			req.SetPathValue("slug", "noel")
			req.SetPathValue("token", "lien")
			rec := httptest.NewRecorder()
			r.Handler.ServeHTTP(rec, req)
			if rec.Code != http.StatusMethodNotAllowed {
				t.Errorf("%s %s : 405 attendu, %d", m, r.Pattern, rec.Code)
			}
		}
	}
	if n != 2 {
		t.Fatalf("routes modifiantes : %d", n)
	}
	// Le formulaire : un GET n'enregistre rien.
	for _, m := range []string{http.MethodGet, http.MethodHead} {
		rec := httptest.NewRecorder()
		e.handler.ServeHTTP(rec, httptest.NewRequest(m, joinPath+"?prenom=a&nom=b&email=c@d.fr", nil))
	}
	e.do(t, http.MethodGet, "/noel/inscription/confirmer/lien", nil, "fr")
	if e.invites.count() != 0 || e.verify.total() != 0 {
		t.Fatal("un GET a modifié l'état")
	}
}

func TestRoutesDeclareTheirMethodsConsistently(t *testing.T) {
	e := newEnv(t)
	for _, r := range e.pages.Routes() {
		if strings.HasPrefix(r.Pattern, "POST ") != r.Modifies {
			t.Errorf("%s : Modifies=%v incohérent", r.Pattern, r.Modifies)
		}
	}
}

// Une requête modifiante sans jeton CSRF est refusée par la chaîne de F2c.
func TestPostWithoutCSRFTokenIsRefused(t *testing.T) {
	e := newEnv(t)
	req := httptest.NewRequest(http.MethodPost, "/noel/inscription/code", strings.NewReader(url.Values{"ref": {"ref-1"}, "code": {"ABC123"}}.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Sec-Fetch-Site", "same-origin")
	rec := httptest.NewRecorder()
	e.handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden || e.verify.total() != 0 {
		t.Fatalf("POST sans jeton : %d", rec.Code)
	}
}

var (
	inlineStyleRe  = regexp.MustCompile(`(?i)<style[\s>]|\sstyle\s*=`)
	inlineScriptRe = regexp.MustCompile(`(?i)<script(\s[^>]*)?>[^<]|\son[a-z]+\s*=`)
	dataURLRe      = regexp.MustCompile(`(?i)data:[a-z]+/|url\(\s*["']?data:`)
	dialogRe       = regexp.MustCompile(`\b(alert|confirm)\(`)
)

func TestTemplatesAndAssetsHaveNoInlineStyleScriptOrDataURL(t *testing.T) {
	check := func(name, src string) {
		if inlineStyleRe.MatchString(src) {
			t.Errorf("%s : style en ligne", name)
		}
		if inlineScriptRe.MatchString(src) {
			t.Errorf("%s : script ou gestionnaire en ligne", name)
		}
		if dataURLRe.MatchString(src) {
			t.Errorf("%s : ressource data:", name)
		}
		if dialogRe.MatchString(src) {
			t.Errorf("%s : boîte du navigateur", name)
		}
		for _, p := range websec.CheckTemplate(name, src) {
			t.Errorf("%s : %s", name, p)
		}
	}
	count := 0
	for _, dir := range []string{"templates/registration/public", "static/css/registration/public"} {
		var fsys fs.FS = web.Templates
		if strings.HasPrefix(dir, "static") {
			fsys = web.Static
		}
		entries, err := fs.ReadDir(fsys, dir)
		if err != nil {
			t.Fatal(err)
		}
		for _, en := range entries {
			b, err := fs.ReadFile(fsys, dir+"/"+en.Name())
			if err != nil {
				t.Fatal(err)
			}
			check(dir+"/"+en.Name(), string(b))
			count++
		}
	}
	if count < 6 {
		t.Fatalf("fichiers vérifiés : %d", count)
	}
}

func TestRenderedPagesHaveNoInlineStyleOrDataURL(t *testing.T) {
	e := newEnv(t)
	e.group.BudgetCents = cents(2500)
	for _, p := range []string{joinPath, "/noel/inscription/confirmer/lien"} {
		rec := e.do(t, http.MethodGet, p, nil, "fr")
		if rec.Code != 200 {
			t.Fatalf("%s : %d", p, rec.Code)
		}
		body := rec.Body.String()
		if inlineStyleRe.MatchString(body) || dataURLRe.MatchString(body) || strings.Contains(body, "<script>") {
			t.Errorf("%s : contenu interdit par la CSP", p)
		}
	}
	for _, r := range []*httptest.ResponseRecorder{
		e.do(t, http.MethodPost, "/noel/inscription/code", codeForm("ref-1", "ABC123"), "fr"),
		e.do(t, http.MethodPost, "/noel/inscription/code", codeForm("ref-1", "ZZZZZZ"), "fr"),
	} {
		body := r.Body.String()
		if inlineStyleRe.MatchString(body) || dataURLRe.MatchString(body) || strings.Contains(body, "<script>") {
			t.Error("page de code ou de réussite : contenu interdit par la CSP")
		}
	}
}
