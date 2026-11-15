package targetpage

import (
	"io/fs"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"
	"time"

	"secretsanta/internal/websec"
	"secretsanta/web"
)

var (
	inlineStyleRe = regexp.MustCompile(`(?is)<[a-z][^>]*\sstyle\s*=|<style\b`)
	dataURLRe     = regexp.MustCompile(`(?i)data:`)
)

// Un GET (ou toute méthode sûre) sur une route qui modifie l'état renvoie 405
// sans rien modifier.
func TestStateChangingRoutesRefuseSafeMethods(t *testing.T) {
	e := newEnv(t)
	s := newScene(t, e, "csp-methodes", nil, false)
	pid := s.normal[0]
	e.login(t, s.gid, pid)
	e.setBudget(t, s.gid, ptr(2000), 2)
	for _, h := range []struct {
		name string
		path string
	}{{"preference", "/preference"}, {"budget-vu", "/budget-vu"}} {
		for _, m := range []string{http.MethodGet, http.MethodHead, http.MethodOptions, http.MethodTrace} {
			w := e.do(m, "/"+s.slug+h.path+"?un_seul=1&version=2", "", nil)
			if w.Code != http.StatusMethodNotAllowed {
				t.Errorf("%s %s : %d, 405 attendu", m, h.name, w.Code)
			}
		}
	}
	if r := e.role(t, s.gid, pid); r != "normal" {
		t.Errorf("un GET a modifié le rôle : %q", r)
	}
	if v, _ := e.store.BudgetSeen(ctx, s.gid, pid); v != 0 {
		t.Errorf("un GET a marqué le budget comme vu : %d", v)
	}
}

// Les gestionnaires eux-mêmes (sans mux ni CSRF) refusent les méthodes sûres.
func TestHandlersAreWrappedByRequireUnsafeMethod(t *testing.T) {
	e := newEnv(t)
	for name, h := range map[string]http.Handler{"preference": e.pages.Preference(), "budget-vu": e.pages.BudgetSeen()} {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/x/"+name, nil))
		if w.Code != http.StatusMethodNotAllowed {
			t.Errorf("%s : %d", name, w.Code)
		}
	}
}

// Un POST sans jeton CSRF est refusé avant le gestionnaire.
func TestPostWithoutCSRFTokenRefused(t *testing.T) {
	e := newEnv(t)
	s := newScene(t, e, "csp-csrf", nil, false)
	e.login(t, s.gid, s.normal[0])
	w := e.do("POST", "/"+s.slug+"/preference", "", url.Values{"un_seul": {"1"}})
	if w.Code != http.StatusForbidden {
		t.Errorf("POST sans jeton : %d, 403 attendu", w.Code)
	}
	if r := e.role(t, s.gid, s.normal[0]); r != "normal" {
		t.Errorf("rôle %q", r)
	}
}

// Sans session participant : renvoi vers la connexion, rien n'est modifié.
func TestNoSessionRedirectsToLogin(t *testing.T) {
	e := newEnv(t)
	s := newScene(t, e, "csp-sans-session", nil, false)
	w := e.do("GET", "/"+s.slug+"/participant", "", nil)
	if w.Code != 303 || w.Header().Get("Location") != "/"+s.slug+"/connexion" {
		t.Errorf("sans session : %d %q", w.Code, w.Header().Get("Location"))
	}
	w = e.post(t, s.slug, "/preference", "", url.Values{"un_seul": {"1"}})
	if w.Code != 303 || e.role(t, s.gid, s.normal[0]) != "normal" {
		t.Errorf("POST sans session : %d", w.Code)
	}
}

// La session d'un autre groupe ne donne rien ici.
func TestOtherGroupSessionRefused(t *testing.T) {
	e := newEnv(t)
	a := newScene(t, e, "csp-groupe-a", nil, false)
	b := newScene(t, e, "csp-groupe-b", nil, false)
	e.login(t, a.gid, a.normal[0])
	w := e.do("GET", "/"+b.slug+"/participant", "", nil)
	if w.Code != 303 {
		t.Errorf("session d'un autre groupe : %d", w.Code)
	}
}

func TestUnknownGroupNeutral404(t *testing.T) {
	e := newEnv(t)
	w := e.do("GET", "/inconnu-du-tout/participant", "", nil)
	if w.Code != 404 {
		t.Errorf("groupe inconnu : %d", w.Code)
	}
}

func TestTemplatesRespectStrictCSP(t *testing.T) {
	n := 0
	err := fs.WalkDir(web.Templates, "templates/draw", func(p string, d fs.DirEntry, err error) error {
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
	for _, f := range []string{"static/css/draw/draw.css", "static/js/draw/countdown.js"} {
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
	pages := pagesEnglish(t, e)
	s := newScene(t, e, "csp-rendu", at(2*time.Hour), true)
	e.login(t, s.gid, s.normal[0])
	pages["wait fr"] = e.do("GET", "/csp-rendu/participant", "", nil).Body.String()
	for name, body := range pages {
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
