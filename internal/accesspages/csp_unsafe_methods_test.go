package accesspages

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

func TestStateChangingRoutesRejectGet(t *testing.T) {
	e := newEnv(t)
	g := e.createGroup(t, "noel")
	e.magic.valid["t"] = 1
	n := 0
	for _, r := range e.pages.Routes() {
		if !r.Modifies {
			continue
		}
		n++
		method, path, _ := strings.Cut(r.Pattern, " ")
		path = strings.NewReplacer("{slug}", "noel", "{token}", "t").Replace(path)
		for _, m := range []string{http.MethodGet, http.MethodHead, http.MethodOptions} {
			req := httptest.NewRequest(m, path, nil)
			req.SetPathValue("slug", "noel")
			req.SetPathValue("token", "t")
			rec := httptest.NewRecorder()
			r.Handler.ServeHTTP(rec, req)
			if rec.Code != http.StatusMethodNotAllowed {
				t.Errorf("%s %s : 405 attendu, %d", m, r.Pattern, rec.Code)
			}
		}
		_ = method
	}
	if n < 5 {
		t.Fatalf("routes modifiantes trouvées : %d", n)
	}
	if e.sessions.count() != 0 || len(e.magic.requests) != 0 || len(e.magic.valid) != 1 {
		t.Fatal("un GET a modifié l'état")
	}
	_ = g
}

func TestRoutesDeclareTheirMethodsConsistently(t *testing.T) {
	e := newEnv(t)
	for _, r := range e.pages.Routes() {
		if strings.HasPrefix(r.Pattern, "POST ") != r.Modifies {
			t.Errorf("%s : Modifies=%v incohérent", r.Pattern, r.Modifies)
		}
	}
	_ = websec.RequireUnsafeMethod
}

var (
	inlineStyleRe = regexp.MustCompile(`(?i)<style[\s>]|\sstyle\s*=`)
	inlineScript  = regexp.MustCompile(`(?i)<script(\s[^>]*)?>[^<]|\son[a-z]+\s*=`)
	dataURLRe     = regexp.MustCompile(`(?i)data:[a-z]+/|url\(\s*["']?data:`)
)

func TestTemplatesAndAssetsHaveNoInlineStyleScriptOrDataURL(t *testing.T) {
	check := func(name, src string) {
		if inlineStyleRe.MatchString(src) {
			t.Errorf("%s : style en ligne", name)
		}
		if inlineScript.MatchString(src) {
			t.Errorf("%s : script ou gestionnaire en ligne", name)
		}
		if dataURLRe.MatchString(src) {
			t.Errorf("%s : ressource data:", name)
		}
		for _, p := range websec.CheckTemplate(name, src) {
			t.Errorf("%s : %s", name, p)
		}
	}
	count := 0
	for _, dir := range []string{"templates/access", "static/css/access", "static/js/access"} {
		var fsys fs.FS = web.Templates
		if dir != "templates/access" {
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
	if count < 5 {
		t.Fatalf("fichiers vérifiés : %d", count)
	}
}

func TestRenderedPagesHaveNoInlineStyleOrDataURL(t *testing.T) {
	e := newEnv(t)
	e.createGroup(t, "noel")
	for _, p := range []string{"/noel/connexion", "/noel/admin/connexion", "/noel/connexion/lien/abc", "/noel/acces/abc"} {
		rec := e.do(t, http.MethodGet, p, nil, "198.51.100.1", "fr")
		if rec.Code != 200 {
			t.Fatalf("%s : %d", p, rec.Code)
		}
		body := rec.Body.String()
		if inlineStyleRe.MatchString(body) || dataURLRe.MatchString(body) || strings.Contains(body, "<script>") {
			t.Errorf("%s : contenu interdit par la CSP", p)
		}
	}
}
