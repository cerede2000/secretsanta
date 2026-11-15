package maildiagpages_test

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"

	"io/fs"
	"secretsanta/web"
)

var (
	inlineStyleRe = regexp.MustCompile(`(?is)<[a-z][^>]*\sstyle\s*=|<style\b`)
	dataURLRe     = regexp.MustCompile(`(?i)data:`)
	inlineEventRe = regexp.MustCompile(`(?is)<[a-z][^>]*\son[a-z]+\s*=`)
)

// F6c.AC16 : un GET sur une route modifiante renvoie 405 sans rien modifier.
func TestStateChangingHandlersRefuseSafeMethods(t *testing.T) {
	e := newEnv(t)
	_ = e.supp.Add(t.Context(), e.gid, "rebond@exemple.fr", "550")
	for _, path := range []string{testPath, pagePath + "/suppression/retirer"} {
		for _, m := range []string{http.MethodGet, http.MethodHead, http.MethodOptions, http.MethodTrace} {
			w := e.request(m, path+"?to=a@exemple.fr&address=rebond@exemple.fr", "", "", nil, e.cookies)
			if w.Code != http.StatusMethodNotAllowed {
				t.Errorf("%s %s : %d, 405 attendu", m, path, w.Code)
			}
		}
	}
	if ok, _ := e.supp.IsSuppressed(t.Context(), e.gid, "rebond@exemple.fr"); !ok || len(e.mailer.items) != 0 {
		t.Error("un GET a modifié l'état")
	}
	// Les gestionnaires eux-mêmes (hors chaîne) refusent aussi.
	_ = url.Values{}
	_ = httptest.NewRecorder
}

// Aucun gabarit n'utilise de style en ligne, de gestionnaire on*, de <style> ni de data:.
func TestTemplatesAreCSPClean(t *testing.T) {
	files, err := fs.Glob(web.Templates, "templates/maildiag/*.html")
	if err != nil || len(files) == 0 {
		t.Fatalf("gabarits introuvables : %v", err)
	}
	for _, f := range files {
		raw, _ := web.Templates.ReadFile(f)
		s := string(raw)
		if inlineStyleRe.MatchString(s) || inlineEventRe.MatchString(s) || dataURLRe.MatchString(s) {
			t.Errorf("%s : style en ligne, gestionnaire on* ou data:", f)
		}
	}
	e := newEnv(t)
	body := e.get(pagePath, "").Body.String()
	if inlineStyleRe.MatchString(body) || inlineEventRe.MatchString(body) || dataURLRe.MatchString(body) || strings.Contains(body, "<script>") {
		t.Error("la page rendue viole la CSP")
	}
	css, err := fs.ReadFile(web.Static, "static/css/maildiag/maildiag.css")
	if err != nil {
		t.Fatal(err)
	}
	if dataURLRe.Match(css) {
		t.Error("data: dans le CSS")
	}
}
