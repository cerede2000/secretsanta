package people

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
	a := e.person(t, e.groupA, "Alice", "alice@exemple.fr", participants.RoleNormal)
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
		path = strings.ReplaceAll(strings.ReplaceAll(path, "{slug}", "noel"), "{id}", sid(a))
		for _, m := range []string{http.MethodGet, http.MethodHead, http.MethodOptions} {
			req := httptest.NewRequest(m, path+"?confirm=1&kind=exclusion&email=x@y.fr", nil)
			req.SetPathValue("slug", "noel")
			req.SetPathValue("id", sid(a))
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
	if n < 8 {
		t.Fatalf("routes modifiantes trouvées : %d", n)
	}
	// Par la chaîne complète : un GET sur une route modifiante ne change rien.
	rec := e.get(t, "/noel/admin/participants/"+sid(a)+"/supprimer?confirm=1", "fr", ck)
	if rec.Code == 200 || rec.Code == 303 {
		t.Errorf("GET de suppression : %d", rec.Code)
	}
	var count int
	if err := e.db.QueryRow(`SELECT COUNT(*) FROM participants`).Scan(&count); err != nil || count != 1 {
		t.Fatalf("un GET a modifié l'état : %d %v", count, err)
	}
	// Un POST sans jeton CSRF est refusé.
	req := httptest.NewRequest(http.MethodPost, "/noel/admin/participants/"+sid(a)+"/supprimer", strings.NewReader("confirm=1"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Sec-Fetch-Site", "same-origin")
	for _, c := range ck {
		req.AddCookie(c)
	}
	out := httptest.NewRecorder()
	e.handler.ServeHTTP(out, req)
	if out.Code != http.StatusForbidden {
		t.Errorf("POST sans CSRF : 403 attendu, %d", out.Code)
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
		{web.Templates, "templates/admin/people"},
		{web.Static, "static/css/admin/people"},
		{web.Static, "static/js/admin/people"},
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
	if count < 6 {
		t.Fatalf("fichiers vérifiés : %d", count)
	}
}

func TestRenderedPagesHaveNoInlineStyleOrDataURL(t *testing.T) {
	e := newEnv(t)
	for name, body := range pagesInEveryState(t, e, "fr") {
		checkCSP(t, "page "+name, body)
	}
}
