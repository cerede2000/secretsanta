package mailboxpage

import (
	"html"
	"regexp"
	"sort"
	"strings"
	"testing"

	"secretsanta/web"
)

var rawKeyRe = regexp.MustCompile(`\b(?:mailbox|layout|core|i18n)\.[a-z_0-9]+(?:\.[a-z_0-9]+)+`)

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
		if !strings.HasPrefix(k, "mailbox.") {
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

// Toute clé que les gabarits emploient existe dans les deux catalogues, et
// aucun gabarit n'emploie de clé d'un autre module que le layout ou le noyau.
func TestTemplateKeysExist(t *testing.T) {
	fr, en := catalogFile(t, "fr"), catalogFile(t, "en")
	keyRe := regexp.MustCompile(`\(?t "(mailbox\.[a-z_0-9.]+)"`)
	entries, err := web.Templates.ReadDir("templates/mailbox")
	if err != nil || len(entries) == 0 {
		t.Fatalf("gabarits : %v", err)
	}
	used := map[string]bool{}
	for _, f := range entries {
		b, _ := web.Templates.ReadFile("templates/mailbox/" + f.Name())
		for _, m := range keyRe.FindAllStringSubmatch(string(b), -1) {
			used[m[1]] = true
			if _, ok := fr[m[1]]; !ok {
				t.Errorf("%s : clé %q absente du français", f.Name(), m[1])
			}
			if _, ok := en[m[1]]; !ok {
				t.Errorf("%s : clé %q absente de l'anglais", f.Name(), m[1])
			}
		}
	}
	for k := range fr {
		if !used[k] && k != "mailbox.error.title" { // employée par le Go (TitleKey)
			t.Errorf("clé %q jamais employée par un gabarit", k)
		}
	}
}

func pagesEnglish(t *testing.T, e *env) map[string]string {
	t.Helper()
	out := map[string]string{}
	s := newScene(t, e, "en-boite")
	e.discover(t, s.gid, s.more[0], -1)
	e.login(t, s.gid, s.more[0])
	out["two"] = e.do("GET", "/en-boite/boite", "en", nil).Body.String()
	e.discover(t, s.gid, s.normal[0], -1)
	e.login(t, s.gid, s.normal[0])
	out["one"] = e.do("GET", "/en-boite/boite", "en", nil).Body.String()
	e.login(t, s.gid, s.reception[0])
	out["reception"] = e.do("GET", "/en-boite/boite", "en", nil).Body.String()
	out["notfound"] = e.do("GET", "/en-absent/boite", "en", nil).Body.String()
	return out
}

func TestEnglishRenderHasNoRawKeyNorFrench(t *testing.T) {
	e := newEnv(t)
	fr, en := catalogFile(t, "fr"), catalogFile(t, "en")
	pages := pagesEnglish(t, e)
	for name, body := range pages {
		if !strings.Contains(body, `<html lang="en">`) {
			t.Errorf("%s : lang=en absent", name)
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
	for name, want := range map[string]string{
		"two":       "Letter received · gift 2 of 2",
		"one":       en["mailbox.received.title"],
		"reception": en["mailbox.note.reception"],
		"notfound":  "not found",
	} {
		if !strings.Contains(strings.ToLower(html.UnescapeString(pages[name])), strings.ToLower(want)) {
			t.Errorf("%s : texte anglais %q absent", name, want)
		}
	}
}

func TestFrenchRender(t *testing.T) {
	e := newEnv(t)
	fr := catalogFile(t, "fr")
	s := newScene(t, e, "boite-fr")
	e.login(t, s.gid, s.normal[0])
	body := html.UnescapeString(e.do("GET", "/boite-fr/boite", "fr", nil).Body.String())
	for _, want := range []string{fr["mailbox.title"], fr["mailbox.own.title"], fr["mailbox.info.lead"], `<html lang="fr">`} {
		if !strings.Contains(body, want) {
			t.Errorf("texte français %q absent", want)
		}
	}
}

// Le script ne contient aucun texte à afficher.
func TestScriptHasNoHardcodedText(t *testing.T) {
	b, err := web.Static.ReadFile("static/js/mailbox/mailbox.js")
	if err != nil {
		t.Fatal(err)
	}
	src := string(b)
	for _, banned := range []string{"lettre", "Père Noël", "Santa", "textContent", "innerHTML", "innerText"} {
		if strings.Contains(src, banned) {
			t.Errorf("texte ou injection %q dans le script", banned)
		}
	}
}
