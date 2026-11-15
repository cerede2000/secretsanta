package targetpage

import (
	"html"
	"regexp"
	"sort"
	"strings"
	"testing"
	"time"

	"secretsanta/web"
)

var rawKeyRe = regexp.MustCompile(`\b(?:draw|layout|core|i18n)\.[a-z_0-9]+(?:\.[a-z_0-9]+)+`)

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
		if !strings.HasPrefix(k, "draw.") {
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

// Toute clé que les gabarits emploient existe dans les deux catalogues.
func TestTemplateKeysExist(t *testing.T) {
	fr, en := catalogFile(t, "fr"), catalogFile(t, "en")
	keyRe := regexp.MustCompile(`\(?t "(draw\.[a-z_0-9.]+)"`)
	entries, err := web.Templates.ReadDir("templates/draw")
	if err != nil || len(entries) == 0 {
		t.Fatalf("gabarits : %v", err)
	}
	for _, en0 := range entries {
		b, _ := web.Templates.ReadFile("templates/draw/" + en0.Name())
		for _, m := range keyRe.FindAllStringSubmatch(string(b), -1) {
			if _, ok := fr[m[1]]; !ok {
				t.Errorf("%s : clé %q absente du français", en0.Name(), m[1])
			}
			if _, ok := en[m[1]]; !ok {
				t.Errorf("%s : clé %q absente de l'anglais", en0.Name(), m[1])
			}
		}
	}
}

// pagesEnglish rend chaque page du paquet en anglais.
func pagesEnglish(t *testing.T, e *env) map[string]string {
	t.Helper()
	out := map[string]string{}
	wait := newScene(t, e, "en-wait", at(50*time.Hour), true)
	e.setBudget(t, wait.gid, ptr(2500), 1)
	e.setEventDate(t, wait.gid, "2026-12-24")
	e.login(t, wait.gid, wait.normal[0])
	out["wait"] = e.do("GET", "/en-wait/participant", "en", nil).Body.String()
	e.login(t, wait.gid, wait.more[0])
	out["wait offre_plus"] = e.do("GET", "/en-wait/participant", "en", nil).Body.String()
	e.login(t, wait.gid, wait.normal[0])
	out["wait locked error"] = e.post(t, "en-wait", "/preference", "en", map[string][]string{"un_seul": {"1"}}).Body.String()

	pre := newScene(t, e, "en-pre", nil, false)
	e.login(t, pre.gid, pre.normal[0])
	out["not drawn"] = e.do("GET", "/en-pre/participant", "en", nil).Body.String()

	card := newScene(t, e, "en-card", nil, true)
	e.setBudget(t, card.gid, ptr(2500), 1)
	e.setEventDate(t, card.gid, "2026-12-24")
	e.discover(t, card.gid, card.more[0], 0)
	e.login(t, card.gid, card.more[0])
	out["two cards"] = e.do("GET", "/en-card/ma-cible", "en", nil).Body.String()
	e.discover(t, card.gid, card.normal[0], -1)
	e.login(t, card.gid, card.normal[0])
	out["one card"] = e.do("GET", "/en-card/ma-cible", "en", nil).Body.String()

	e.login(t, card.gid, card.reception[0])
	out["reception"] = e.do("GET", "/en-card/participant", "en", nil).Body.String()
	out["notfound"] = e.do("GET", "/en-missing/participant", "en", nil).Body.String()
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
	checks := map[string]string{
		"wait":              en["draw.wait.title.ready"],
		"wait locked error": en["draw.pref.error.locked"],
		"not drawn":         en["draw.wait.title.notdone"],
		"two cards":         "Gift 2 of 2",
		"one card":          en["draw.card.gives"],
		"reception":         en["draw.reception.title"],
		"notfound":          "not found",
	}
	for name, want := range checks {
		if !strings.Contains(strings.ToLower(html.UnescapeString(pages[name])), strings.ToLower(want)) {
			t.Errorf("%s : texte anglais %q absent", name, want)
		}
	}
	if !strings.Contains(pages["wait"], "December 24") || !strings.Contains(pages["wait"], "November 17 at 12:00") {
		t.Errorf("dates anglaises mal écrites : %s", pages["wait"])
	}
}

func TestFrenchRender(t *testing.T) {
	e := newEnv(t)
	fr := catalogFile(t, "fr")
	s := newScene(t, e, "famille-fr", at(50*time.Hour), true)
	e.setEventDate(t, s.gid, "2026-12-24")
	e.setBudget(t, s.gid, ptr(2500), 1)
	e.login(t, s.gid, s.normal[0])
	body := html.UnescapeString(e.do("GET", "/famille-fr/participant", "fr", nil).Body.String())
	for _, want := range []string{fr["draw.wait.title.ready"], fr["draw.pref.label"], "24 décembre", "17 novembre à 12:00", `<html lang="fr">`} {
		if !strings.Contains(body, want) {
			t.Errorf("texte français %q absent", want)
		}
	}
}

// Le script du compte à rebours ne contient aucun texte à afficher : les
// textes viennent du gabarit (attributs data-*).
func TestCountdownScriptHasNoHardcodedText(t *testing.T) {
	b, err := web.Static.ReadFile("static/js/draw/countdown.js")
	if err != nil {
		t.Fatal(err)
	}
	src := string(b)
	for _, banned := range []string{"jours", "heures", "minutes restantes", "Temps restant", "days left"} {
		if strings.Contains(src, banned) {
			t.Errorf("texte %q écrit en dur dans le script", banned)
		}
	}
	if !strings.Contains(src, "dataset.tLabel") {
		t.Error("l'étiquette doit venir de data-t-label")
	}
}
