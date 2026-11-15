package suggestionpages

import (
	"html"
	"regexp"
	"sort"
	"strings"
	"testing"

	"secretsanta/internal/access/limiter"
	"secretsanta/internal/suggestions"
	"secretsanta/web"
)

var rawKeyRe = regexp.MustCompile(`\b(?:suggestions|layout|core|i18n)\.[a-z_0-9]+(?:\.[a-z_0-9]+)+`)

var paramRe = regexp.MustCompile(`\{([A-Za-z_]+)\}`)

func paramsOf(s string) string {
	var ps []string
	for _, m := range paramRe.FindAllStringSubmatch(s, -1) {
		ps = append(ps, m[1])
	}
	sort.Strings(ps)
	return strings.Join(ps, ",")
}

func TestCatalogsParity(t *testing.T) {
	fr, en := catalogFile(t, "fr"), catalogFile(t, "en")
	if len(fr) == 0 {
		t.Fatal("catalogue français vide")
	}
	for k, v := range fr {
		if !strings.HasPrefix(k, "suggestions.") {
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

// Toute clé que les gabarits emploient (ou que le Go nomme) existe dans les
// deux catalogues.
func TestTemplateKeysExist(t *testing.T) {
	fr, en := catalogFile(t, "fr"), catalogFile(t, "en")
	keyRe := regexp.MustCompile(`"(suggestions\.[a-z_0-9.]+)"`)
	entries, err := web.Templates.ReadDir("templates/suggestions")
	if err != nil || len(entries) == 0 {
		t.Fatalf("gabarits : %v", err)
	}
	check := func(where, src string) {
		for _, m := range keyRe.FindAllStringSubmatch(src, -1) {
			if _, ok := fr[m[1]]; !ok {
				t.Errorf("%s : clé %q absente du français", where, m[1])
			}
			if _, ok := en[m[1]]; !ok {
				t.Errorf("%s : clé %q absente de l'anglais", where, m[1])
			}
		}
	}
	for _, e0 := range entries {
		b, _ := web.Templates.ReadFile("templates/suggestions/" + e0.Name())
		check(e0.Name(), string(b))
	}
	// Les clés choisies par le Go.
	b, err := readPagesGo()
	if err != nil {
		t.Fatal(err)
	}
	check("pages.go", b)
}

// pagesEnglish rend la page dans chacun de ses états, en anglais.
func pagesEnglish(t *testing.T, e *env) map[string]string {
	t.Helper()
	out := map[string]string{}
	get := func(name, path string) {
		out[name] = e.do("GET", path, "en", nil).Body.String()
	}
	e.login(t, giver2)
	e.groups.g.BudgetCents = ptr(2500)
	get("budget two gifts", "/famille/suggestions/101")
	get("budget second gift", "/famille/suggestions/102")
	get("budget filter", "/famille/suggestions/101?min=15&max=25")
	get("bad min", "/famille/suggestions/101?min=abc")
	get("bad order", "/famille/suggestions/101?min=30&max=20")
	e.groups.g.BudgetCents = nil
	get("free", "/famille/suggestions/101")
	e.suggest.res = &suggestions.Result{FromLetter: true, Throttled: true, Items: goodResult().Items}
	get("throttled letter", "/famille/suggestions/101")
	e.suggest.res = &suggestions.Result{Unavailable: true}
	get("unavailable", "/famille/suggestions/101")
	e.suggest.res = &suggestions.Result{}
	get("empty", "/famille/suggestions/101")
	e.suggest.res = goodResult()
	e.suggest.err = limiter.Error()
	get("limited", "/famille/suggestions/101")
	out["refresh limited"] = e.post(t, "/famille/suggestions/101/actualiser", "en", nil).Body.String()
	e.suggest.err = nil
	get("other pair", "/famille/suggestions/999")

	e.login(t, giver1)
	get("one gift", "/famille/suggestions/103")
	e.login(t, recepOnl)
	get("reception", "/famille/suggestions/103")
	get("unknown group", "/inconnu/suggestions/103")
	return out
}

func TestEnglishRenderHasNoRawKeyNorFrench(t *testing.T) {
	e := newEnv(t)
	fr, en := catalogFile(t, "fr"), catalogFile(t, "en")
	pages := pagesEnglish(t, e)
	for name, body := range pages {
		if !strings.Contains(body, `<html lang="en">`) {
			t.Errorf("%s : lang=en absent", name)
			continue
		}
		text := html.UnescapeString(body)
		for k, v := range fr {
			if strings.Contains(body, k) {
				t.Errorf("%s : clé brute %q dans le rendu", name, k)
			}
			if v != en[k] && len(v) > 4 && !strings.Contains(v, "{") && strings.Contains(text, v) {
				t.Errorf("%s : texte français %q dans le rendu anglais", name, v)
			}
		}
		if m := rawKeyRe.FindString(body); m != "" {
			t.Errorf("%s : clé brute %q dans le rendu", name, m)
		}
	}
	checks := map[string]string{
		"budget two gifts": en["suggestions.gift.group"],
		"free":             en["suggestions.free.title_free"],
		"bad min":          en["suggestions.free.error.min"],
		"bad order":        en["suggestions.free.error.order"],
		"unavailable":      en["suggestions.unavailable.title"],
		"empty":            en["suggestions.empty.title"],
		"throttled letter": en["suggestions.notice.throttled"],
		"limited":          en["suggestions.error.limited"],
		"other pair":       en["suggestions.error.not_found"],
		"reception":        en["suggestions.error.no_page"],
		"one gift":         en["suggestions.privacy"],
	}
	for name, want := range checks {
		if !strings.Contains(html.UnescapeString(pages[name]), want) {
			t.Errorf("%s : texte anglais %q absent", name, want)
		}
	}
	if !strings.Contains(pages["budget two gifts"], "€25") {
		t.Errorf("budget anglais mal écrit : %s", pages["budget two gifts"])
	}
}

func TestFrenchRender(t *testing.T) {
	e := newEnv(t)
	fr := catalogFile(t, "fr")
	e.login(t, giver2)
	e.groups.g.BudgetCents = ptr(2500)
	body := html.UnescapeString(e.do("GET", "/famille/suggestions/101", "fr", nil).Body.String())
	for _, want := range []string{fr["suggestions.title"], "Cadeau 1 · Léa", "Cadeau 2 · Noé", "budget de 25 €", "24,99 €", `<html lang="fr">`, "Jusqu'à 15 €", "15 € à 25 €"} {
		if !strings.Contains(body, want) {
			t.Errorf("texte français %q absent", want)
		}
	}
}
