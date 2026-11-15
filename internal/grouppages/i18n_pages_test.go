package grouppages

import (
	"html"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// rawKeyRe reconnaît une clé de texte laissée telle quelle (groups.create.name…).
var rawKeyRe = regexp.MustCompile(`\b(?:groups|layout|core|i18n)\.[a-z_]+(?:\.[a-z_]+)+`)

var paramRe = regexp.MustCompile(`\{([A-Za-z_]+)\}`)

func paramsOf(s string) string {
	var ps []string
	for _, m := range paramRe.FindAllStringSubmatch(s, -1) {
		ps = append(ps, m[1])
	}
	sort.Strings(ps)
	return strings.Join(ps, ",")
}

// Les catalogues fr et en du module ont les mêmes clés et les mêmes paramètres,
// toutes préfixées par le module.
func TestCatalogsParity(t *testing.T) {
	fr, en := catalogFile(t, "fr"), catalogFile(t, "en")
	if len(fr) == 0 {
		t.Fatal("catalogue français vide")
	}
	for k, v := range fr {
		if !strings.HasPrefix(k, "groups.") {
			t.Errorf("clé %q sans préfixe du module", k)
		}
		e, ok := en[k]
		if !ok {
			t.Errorf("clé %q absente en anglais", k)
			continue
		}
		if paramsOf(v) != paramsOf(e) {
			t.Errorf("clé %q : paramètres différents (%q / %q)", k, v, e)
		}
	}
	for k := range en {
		if _, ok := fr[k]; !ok {
			t.Errorf("clé %q absente en français", k)
		}
	}
}

// pagesEnglish rend chaque page du paquet en anglais.
func pagesEnglish(t *testing.T, e *testEnv) map[string]string {
	t.Helper()
	if w := e.postCreate(t, "en", validForm("english-family")); w.Code != 200 {
		t.Fatalf("création : %d %s", w.Code, w.Body.String())
	}
	bad := validForm("english-bad")
	bad.Set("password", "wrong")
	badPw := e.postCreate(t, "en", bad)
	invalid := validForm("x")
	invalid.Set("budget", "abc")
	badFields := e.postCreate(t, "en", invalid)
	created := e.postCreate(t, "en", validForm("english-two"))
	return map[string]string{
		"home":       e.do("GET", "/english-family", "en", nil, nil).Body.String(),
		"notfound":   e.do("GET", "/missing-group", "en", nil, nil).Body.String(),
		"form":       e.do("GET", "/creer", "en", nil, nil).Body.String(),
		"bad pass":   badPw.Body.String(),
		"bad fields": badFields.Body.String(),
		"created":    created.Body.String(),
	}
}

func TestEnglishRenderHasNoRawKeyNorFrench(t *testing.T) {
	e := newEnv(t)
	fr, en := catalogFile(t, "fr"), catalogFile(t, "en")
	pages := pagesEnglish(t, e)
	for name, body := range pages {
		if !strings.Contains(body, `<html lang="en">`) {
			t.Errorf("%s : lang=en absent", name)
		}
		for k, v := range fr {
			if strings.Contains(body, k) {
				t.Errorf("%s : clé brute %q dans le rendu", name, k)
			}
			if v != en[k] && !strings.Contains(v, "{") && strings.Contains(html.UnescapeString(body), v) {
				t.Errorf("%s : texte français %q dans le rendu anglais", name, v)
			}
		}
		if m := rawKeyRe.FindString(body); m != "" {
			t.Errorf("%s : clé brute %q dans le rendu", name, m)
		}
	}
	checks := map[string]string{
		"home":       en["groups.home.welcome"],
		"notfound":   "not found",
		"form":       en["groups.create.submit"],
		"bad pass":   en["groups.create.error.password"],
		"bad fields": en["groups.create.error.validation"],
		"created":    en["groups.created.copy"],
	}
	for name, want := range checks {
		if !strings.Contains(strings.ToLower(html.UnescapeString(pages[name])), strings.ToLower(want)) {
			t.Errorf("%s : texte anglais %q absent", name, want)
		}
	}
}

func TestFrenchRender(t *testing.T) {
	e := newEnv(t)
	fr := catalogFile(t, "fr")
	if w := e.postCreate(t, "", validForm("famille-fr")); w.Code != 200 {
		t.Fatalf("création : %d", w.Code)
	}
	for path, want := range map[string]string{
		"/famille-fr": fr["groups.home.welcome"],
		"/creer":      fr["groups.create.submit"],
	} {
		body := e.do("GET", path, "fr", nil, nil).Body.String()
		if !strings.Contains(body, want) || !strings.Contains(body, `<html lang="fr">`) {
			t.Errorf("%s : texte français %q absent", path, want)
		}
	}
}
