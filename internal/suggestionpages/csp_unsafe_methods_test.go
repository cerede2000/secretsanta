package suggestionpages

import (
	"io/fs"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"

	"secretsanta/internal/suggestions"
	"secretsanta/internal/websec"
	"secretsanta/web"
)

var (
	inlineStyleRe = regexp.MustCompile(`(?is)<[a-z][^>]*\sstyle\s*=|<style\b`)
	dataURLRe     = regexp.MustCompile(`(?i)data:`)
)

// Un GET (ou toute méthode sûre) sur la route qui modifie l'état renvoie 405
// sans rien modifier (aucune recherche, aucun cache touché).
func TestStateChangingRouteRefusesSafeMethods(t *testing.T) {
	e := newEnv(t)
	e.login(t, giver1)
	for _, m := range []string{http.MethodGet, http.MethodHead, http.MethodOptions, http.MethodTrace} {
		w := e.do(m, "/famille/suggestions/103/actualiser", "", nil)
		if w.Code != http.StatusMethodNotAllowed {
			t.Errorf("%s : %d, 405 attendu", m, w.Code)
		}
	}
	if n := e.suggest.count(); n != 0 {
		t.Errorf("une méthode sûre a lancé %d recherche(s)", n)
	}
}

func TestHandlerIsWrappedByRequireUnsafeMethod(t *testing.T) {
	e := newEnv(t)
	w := httptest.NewRecorder()
	e.pages.Refresh().ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/x/suggestions/1/actualiser", nil))
	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("Refresh : %d", w.Code)
	}
}

// L'affichage et le filtre de prix sont des GET : ils ne demandent jamais
// d'actualisation.
func TestShowNeverRefreshes(t *testing.T) {
	e := newEnv(t)
	e.login(t, giver1)
	if w := e.do("GET", "/famille/suggestions/103?min=5&max=50", "", nil); w.Code != 200 {
		t.Fatalf("GET : %d", w.Code)
	}
	if e.suggest.last().Refresh {
		t.Error("un GET a demandé l'actualisation")
	}
}

func TestPostWithoutCSRFTokenRefused(t *testing.T) {
	e := newEnv(t)
	e.login(t, giver1)
	w := e.do("POST", "/famille/suggestions/103/actualiser", "", url.Values{"min": {"1"}})
	if w.Code != http.StatusForbidden {
		t.Errorf("POST sans jeton : %d, 403 attendu", w.Code)
	}
	if e.suggest.count() != 0 {
		t.Error("une recherche a été lancée sans jeton")
	}
}

func TestRefreshPostRefreshesThenRedirects(t *testing.T) {
	e := newEnv(t)
	e.login(t, giver1)
	w := e.post(t, "/famille/suggestions/103/actualiser", "", url.Values{"min": {"10"}, "max": {"30,5"}})
	if w.Code != http.StatusSeeOther {
		t.Fatalf("POST : %d", w.Code)
	}
	if loc := w.Header().Get("Location"); loc != "/famille/suggestions/103?max=30%2C5&min=10" {
		t.Errorf("renvoi vers %q", loc)
	}
	r := e.suggest.last()
	if !r.Refresh || r.MinCents == nil || *r.MinCents != 1000 || r.MaxCents == nil || *r.MaxCents != 3050 {
		t.Errorf("requête %+v", r)
	}
}

func TestNoSessionRedirectsToLogin(t *testing.T) {
	e := newEnv(t)
	w := e.do("GET", "/famille/suggestions/103", "", nil)
	if w.Code != 303 || w.Header().Get("Location") != "/famille/connexion" {
		t.Errorf("sans session : %d %q", w.Code, w.Header().Get("Location"))
	}
	if e.suggest.count() != 0 {
		t.Error("recherche lancée sans session")
	}
}

// Une paire d'une autre personne répond comme une paire inexistante.
func TestOtherGiversPairIsNotFound(t *testing.T) {
	e := newEnv(t)
	e.login(t, giver1)
	for _, pair := range []string{"101", "102", "999", "abc"} {
		if w := e.do("GET", "/famille/suggestions/"+pair, "", nil); w.Code != 404 {
			t.Errorf("paire %s : %d, 404 attendu", pair, w.Code)
		}
	}
	if e.suggest.count() != 0 {
		t.Error("recherche lancée pour la cible d'un autre")
	}
}

func TestReceptionOnlyHasNoPage(t *testing.T) {
	e := newEnv(t)
	e.login(t, recepOnl)
	if w := e.do("GET", "/famille/suggestions/103", "", nil); w.Code != 403 {
		t.Errorf("réception seule : %d, 403 attendu", w.Code)
	}
	if e.suggest.count() != 0 {
		t.Error("recherche lancée pour une réception seule")
	}
}

func TestUndiscoveredTargetRedirectsToDiscovery(t *testing.T) {
	e := newEnv(t)
	e.draw.targets[giver2][1].Discovered = false
	e.login(t, giver2)
	w := e.do("GET", "/famille/suggestions/102", "", nil)
	if w.Code != 303 || w.Header().Get("Location") != "/famille/decouverte" {
		t.Errorf("cible non découverte : %d %q", w.Code, w.Header().Get("Location"))
	}
	body := e.do("GET", "/famille/suggestions/101", "", nil).Body.String()
	mustNotContain(t, body, "Noé")
	mustContain(t, body, "Cadeau 2")
}

func TestGiftSelectorOnlyForTwoTargets(t *testing.T) {
	e := newEnv(t)
	e.login(t, giver2)
	body := e.do("GET", "/famille/suggestions/102", "", nil).Body.String()
	mustContain(t, body, "Cadeau 2 · Noé")
	if e.suggest.last().Gift != 2 {
		t.Errorf("cadeau demandé : %d", e.suggest.last().Gift)
	}
	e.login(t, giver1)
	body = e.do("GET", "/famille/suggestions/103", "", nil).Body.String()
	mustNotContain(t, body, "Cadeau 1")
}

func TestPriceFilterErrorsShowUnderTheField(t *testing.T) {
	e := newEnv(t)
	e.login(t, giver1)
	before := e.suggest.count()
	w := e.do("GET", "/famille/suggestions/103?min=abc", "", nil)
	body := w.Body.String()
	if w.Code != 400 || e.suggest.count() != before {
		t.Fatalf("code %d, recherches %d", w.Code, e.suggest.count()-before)
	}
	for _, want := range []string{`id="pmin-err"`, `aria-invalid="true"`, `aria-describedby="pmin-err"`, `role="alert"`} {
		if !strings.Contains(body, want) {
			t.Errorf("%q absent", want)
		}
	}
}

func TestItemsShowLocalImageAndPrice(t *testing.T) {
	e := newEnv(t)
	e.login(t, giver1)
	body := e.do("GET", "/famille/suggestions/103", "", nil).Body.String()
	mustContain(t, body, `src="/images/abc.jpg"`)
	mustContain(t, body, "24,99 €")
	mustContain(t, body, "Prix à vérifier")
	mustContain(t, body, `href="https://boutique.example/echarpe"`)
	if strings.Contains(body, `src="http`) {
		t.Error("image externe affichée")
	}
}

func TestBudgetFilterIsPassedAsEuros(t *testing.T) {
	e := newEnv(t)
	e.login(t, giver1)
	e.do("GET", "/famille/suggestions/103?min=12.5", "", nil)
	r := e.suggest.last()
	if r.MinCents == nil || *r.MinCents != 1250 || r.MaxCents != nil {
		t.Errorf("filtre %+v", r)
	}
	if r.Gift != 1 {
		t.Errorf("cadeau %d", r.Gift)
	}
	var _ suggestions.Request = r
}

func TestUnknownGroupNeutral404(t *testing.T) {
	e := newEnv(t)
	if w := e.do("GET", "/inconnu/suggestions/1", "", nil); w.Code != 404 {
		t.Errorf("groupe inconnu : %d", w.Code)
	}
}

func TestTemplatesRespectStrictCSP(t *testing.T) {
	n := 0
	err := fs.WalkDir(web.Templates, "templates/suggestions", func(p string, d fs.DirEntry, err error) error {
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
	b, err := web.Static.ReadFile("static/css/suggestions/suggestions.css")
	if err != nil {
		t.Fatal(err)
	}
	if dataURLRe.MatchString(string(b)) || strings.Contains(string(b), "http://") || strings.Contains(string(b), "https://") {
		t.Error("suggestions.css : data: ou ressource distante")
	}
}

func TestRenderedPagesRespectStrictCSP(t *testing.T) {
	e := newEnv(t)
	pages := pagesEnglish(t, e)
	e.login(t, giver2)
	pages["fr"] = e.do("GET", "/famille/suggestions/101", "", nil).Body.String()
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
		if strings.Contains(body, `src="http`) {
			t.Errorf("%s : image distante", name)
		}
	}
}
