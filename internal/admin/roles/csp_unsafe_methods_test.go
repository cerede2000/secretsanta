package roles

import (
	"io/fs"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"

	"secretsanta/internal/participants"
	"secretsanta/internal/websec"
	"secretsanta/web"
)

func TestStateChangingRoutesRejectGet(t *testing.T) {
	e := newEnv(t)
	a := e.person(t, e.groupA, "Alice", participants.RoleNormal)
	ck := e.adminCookies(t, e.groupA)
	n := 0
	for _, r := range e.pages.Routes() {
		if strings.HasPrefix(r.Pattern, "POST ") != r.Modifies {
			t.Errorf("%s : Modifies=%v incohérent", r.Pattern, r.Modifies)
		}
		if !r.Modifies {
			continue
		}
		n++
		_, path, _ := strings.Cut(r.Pattern, " ")
		path = strings.ReplaceAll(path, "{slug}", "noel")
		for _, m := range []string{http.MethodGet, http.MethodHead, http.MethodOptions} {
			req := httptest.NewRequest(m, path+"?ids=1&role=un_seul", nil)
			req.SetPathValue("slug", "noel")
			for _, c := range ck {
				req.AddCookie(c)
			}
			rec := httptest.NewRecorder()
			r.Handler.ServeHTTP(rec, req)
			if rec.Code != http.StatusMethodNotAllowed {
				t.Errorf("%s %s : 405 attendu, %d", m, r.Pattern, rec.Code)
			}
		}
	}
	if n == 0 {
		t.Fatal("aucune route modifiante trouvée")
	}
	// aussi par la chaîne complète (CSRF et sessions)
	rec := e.get(t, "/noel/admin/roles?ids="+ids(a)[0]+"&role=un_seul", "fr", ck)
	if rec.Code != 200 || e.roleOf(t, a) != "normal" {
		t.Fatalf("un GET a modifié l'état : %d %s", rec.Code, e.roleOf(t, a))
	}
}

var (
	inlineStyleRe = regexp.MustCompile(`(?i)<style[\s>]|\sstyle\s*=`)
	inlineScript  = regexp.MustCompile(`(?i)<script(\s[^>]*)?>[^<]|\son[a-z]+\s*=`)
	dataURLRe     = regexp.MustCompile(`(?i)data:[a-z]+/|url\(\s*["']?data:`)
)

func checkCSP(t *testing.T, name, src string) {
	t.Helper()
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

func TestTemplatesAndAssetsHaveNoInlineStyleScriptOrDataURL(t *testing.T) {
	count := 0
	for _, d := range []struct {
		fsys fs.FS
		dir  string
	}{
		{web.Templates, "templates/admin/roles"},
		{web.Static, "static/css/admin/roles"},
		{web.Static, "static/js/admin/roles"},
	} {
		entries, err := fs.ReadDir(d.fsys, d.dir)
		if err != nil {
			t.Fatal(err)
		}
		for _, en := range entries {
			b, err := fs.ReadFile(d.fsys, d.dir+"/"+en.Name())
			if err != nil {
				t.Fatal(err)
			}
			checkCSP(t, d.dir+"/"+en.Name(), string(b))
			count++
		}
	}
	if count < 4 {
		t.Fatalf("fichiers vérifiés : %d", count)
	}
}

func TestRenderedPagesHaveNoInlineStyleOrDataURL(t *testing.T) {
	e := newEnv(t)
	for name, body := range pagesInEveryState(t, e, "fr") {
		checkCSP(t, "page "+name, body)
	}
}
