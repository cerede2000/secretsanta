package mailboxpage

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
)

func rowCounts(t *testing.T, e *env) string {
	t.Helper()
	var out []string
	for _, tbl := range []string{"letters", "letter_ideas", "letterread_stamps", "participants", "draw_pairs"} {
		var n int
		if err := e.db.QueryRow(`SELECT COUNT(*) FROM ` + tbl).Scan(&n); err != nil {
			t.Fatal(err)
		}
		out = append(out, tbl+"="+itoa(int64(n)))
	}
	return strings.Join(out, ",")
}

// La page ne modifie rien : tout verbe autre que GET ou HEAD renvoie 405 sans
// rien changer ; un GET ne change rien non plus.
func TestMailboxIsReadOnlyAndRefusesOtherMethods(t *testing.T) {
	e := newEnv(t)
	s := newScene(t, e, "csp-methodes")
	e.discover(t, s.gid, s.more[0], -1)
	e.login(t, s.gid, s.more[0])
	before := rowCounts(t, e)
	for _, m := range []string{http.MethodOptions, http.MethodTrace} {
		if w := e.do(m, "/"+s.slug+"/boite", "", nil); w.Code != http.StatusMethodNotAllowed {
			t.Errorf("%s : %d, 405 attendu", m, w.Code)
		}
	}
	if w := e.post(t, s.slug, "/boite", "", url.Values{"x": {"1"}}); w.Code != http.StatusMethodNotAllowed {
		t.Errorf("POST : %d, 405 attendu", w.Code)
	}
	for _, m := range []string{http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete} {
		h := httptest.NewRecorder()
		e.pages.Handler().ServeHTTP(h, httptest.NewRequest(m, "/x/boite", nil))
		if h.Code != http.StatusMethodNotAllowed {
			t.Errorf("gestionnaire seul, %s : %d", m, h.Code)
		}
	}
	for i := 0; i < 2; i++ {
		e.do("GET", "/"+s.slug+"/boite", "", nil)
		e.do("HEAD", "/"+s.slug+"/boite", "", nil)
	}
	if after := rowCounts(t, e); after != before {
		t.Errorf("la page a modifié des données : %s -> %s", before, after)
	}
}

func TestPostWithoutCSRFTokenRefused(t *testing.T) {
	e := newEnv(t)
	s := newScene(t, e, "csp-csrf")
	e.login(t, s.gid, s.normal[0])
	if w := e.do("POST", "/"+s.slug+"/boite", "", url.Values{"x": {"1"}}); w.Code != http.StatusForbidden {
		t.Errorf("POST sans jeton : %d, 403 attendu", w.Code)
	}
}

func TestNoSessionRedirectsToLogin(t *testing.T) {
	e := newEnv(t)
	s := newScene(t, e, "csp-sans-session")
	w := e.do("GET", "/"+s.slug+"/boite", "", nil)
	if w.Code != 303 || w.Header().Get("Location") != "/"+s.slug+"/connexion" {
		t.Errorf("sans session : %d %q", w.Code, w.Header().Get("Location"))
	}
	other := newScene(t, e, "csp-autre")
	e.login(t, other.gid, other.normal[0])
	if w := e.do("GET", "/"+s.slug+"/boite", "", nil); w.Code != 303 {
		t.Errorf("session d'un autre groupe : %d", w.Code)
	}
}

func TestUnknownGroupNeutral404(t *testing.T) {
	e := newEnv(t)
	if w := e.do("GET", "/inconnu-du-tout/boite", "", nil); w.Code != 404 {
		t.Errorf("groupe inconnu : %d", w.Code)
	}
}

func TestTemplatesRespectStrictCSP(t *testing.T) {
	n := 0
	err := fs.WalkDir(web.Templates, "templates/mailbox", func(p string, d fs.DirEntry, err error) error {
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
			t.Errorf("%s : attribut ou bloc style en ligne", p)
		}
		if dataURLRe.MatchString(src) {
			t.Errorf("%s : ressource data:", p)
		}
		for _, problem := range websec.CheckTemplate(p, src) {
			t.Error(problem)
		}
		return nil
	})
	if err != nil || n == 0 {
		t.Fatalf("gabarits introuvables (%d) : %v", n, err)
	}
	for _, f := range []string{"static/css/mailbox/mailbox.css", "static/js/mailbox/mailbox.js"} {
		b, err := web.Static.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		if dataURLRe.MatchString(string(b)) || strings.Contains(string(b), "http://") || strings.Contains(string(b), "https://") {
			t.Errorf("%s : data: ou ressource distante", f)
		}
	}
}

func TestRenderedPagesRespectStrictCSP(t *testing.T) {
	e := newEnv(t)
	for name, body := range pagesEnglish(t, e) {
		if inlineStyleRe.MatchString(body) {
			t.Errorf("%s : style en ligne dans le rendu", name)
		}
		if dataURLRe.MatchString(body) {
			t.Errorf("%s : data: dans le rendu", name)
		}
		for _, problem := range websec.CheckTemplate(name, body) {
			t.Error(problem)
		}
		if strings.Contains(body, "src=\"http") || strings.Contains(body, "href=\"http") {
			t.Errorf("%s : ressource distante", name)
		}
	}
}
