package menu

import (
	"html"
	"regexp"
	"sort"
	"strings"
	"testing"
)

var paramRe = regexp.MustCompile(`\{([A-Za-z_]+)\}`)

func paramsOf(s string) string {
	var ps []string
	for _, m := range paramRe.FindAllStringSubmatch(s, -1) {
		ps = append(ps, m[1])
	}
	sort.Strings(ps)
	return strings.Join(ps, ",")
}

var rawKeyRe = regexp.MustCompile(`\b(?:admin\.menu|layout)\.[a-z_]+(?:\.[a-z_]+)*`)

func TestCatalogsParity(t *testing.T) {
	fr, en := catalogFile(t, "fr"), catalogFile(t, "en")
	if len(fr) == 0 {
		t.Fatal("catalogue français vide")
	}
	for k, v := range fr {
		if !strings.HasPrefix(k, "admin.menu.") {
			t.Errorf("clé %q sans préfixe du module", k)
		}
		e, ok := en[k]
		if !ok {
			t.Errorf("clé %q absente en anglais", k)
			continue
		}
		if paramsOf(v) != paramsOf(e) {
			t.Errorf("clé %q : paramètres différents", k)
		}
		if v == e {
			t.Errorf("clé %q identique en français et en anglais", k)
		}
	}
	for k := range en {
		if _, ok := fr[k]; !ok {
			t.Errorf("clé %q absente en français", k)
		}
	}
}

// F10.AC33 : le rendu anglais n'a ni clé brute ni texte français.
func TestEnglishRenderHasNoRawKeyNorFrench(t *testing.T) {
	e := newEnv(t)
	fr, en := catalogFile(t, "fr"), catalogFile(t, "en")
	body := e.do("GET", "/famille/admin/plus", "en", nil, e.cookies["famille"]).Body.String()
	if !strings.Contains(body, `<html lang="en">`) {
		t.Error("lang=en absent")
	}
	text := html.UnescapeString(body)
	for k, v := range fr {
		if strings.Contains(body, k) {
			t.Errorf("clé brute %q", k)
		}
		if v != en[k] && strings.Contains(text, v) {
			t.Errorf("texte français %q dans le rendu anglais", v)
		}
	}
	if m := rawKeyRe.FindString(body); m != "" {
		t.Errorf("clé brute %q", m)
	}
	for _, k := range []string{"settings", "journal", "export", "help", "logout"} {
		if !strings.Contains(text, en["admin.menu."+k]) {
			t.Errorf("texte anglais %q absent", en["admin.menu."+k])
		}
	}
	// Libellés de la barre de navigation en anglais.
	for _, want := range []string{"Dashboard", "Participants", "Draw", "More"} {
		if !strings.Contains(text, want) {
			t.Errorf("libellé de navigation %q absent", want)
		}
	}
}

func TestFrenchRender(t *testing.T) {
	e := newEnv(t)
	fr := catalogFile(t, "fr")
	body := html.UnescapeString(e.do("GET", "/famille/admin/plus", "fr", nil, e.cookies["famille"]).Body.String())
	for _, k := range []string{"settings", "journal", "export", "help", "logout", "note"} {
		if !strings.Contains(body, fr["admin.menu."+k]) {
			t.Errorf("texte français %q absent", fr["admin.menu."+k])
		}
	}
	if !strings.Contains(body, `<html lang="fr">`) {
		t.Error("lang=fr absent")
	}
}
