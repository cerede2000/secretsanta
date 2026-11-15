package admin

import (
	"html"
	"net/url"
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

var rawKeyRe = regexp.MustCompile(`\b(?:registration\.admin|layout)\.[a-z_]+(?:\.[a-z_]+)*`)

func TestCatalogsParity(t *testing.T) {
	fr, en := catalogFile(t, "fr"), catalogFile(t, "en")
	if len(fr) == 0 {
		t.Fatal("catalogue français vide")
	}
	for k, v := range fr {
		if !strings.HasPrefix(k, "registration.admin.") {
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
	}
	for k := range en {
		if _, ok := fr[k]; !ok {
			t.Errorf("clé %q absente en français", k)
		}
	}
}

// Toute clé de texte référencée par le gabarit ou le code existe dans les deux langues.
func TestEveryReferencedKeyExists(t *testing.T) {
	fr, en := catalogFile(t, "fr"), catalogFile(t, "en")
	for _, key := range append(append([]string{}, noticeKeys()...), errKeys()...) {
		if _, ok := fr[key]; !ok {
			t.Errorf("%q absente en français", key)
		}
		if _, ok := en[key]; !ok {
			t.Errorf("%q absente en anglais", key)
		}
	}
}

func noticeKeys() []string {
	var out []string
	for _, k := range notices {
		out = append(out, k)
	}
	return out
}

func errKeys() []string {
	return []string{errorKey(errTooSoon()), "registration.admin.err.limited", "registration.admin.err.notfound",
		"registration.admin.err.capacity", "registration.admin.err.forbidden", "registration.admin.err.generic"}
}

// F11c.AC12 : le rendu anglais n'a ni clé brute ni texte français.
func TestEnglishRenderHasNoRawKeyNorFrench(t *testing.T) {
	e := newEnv(t)
	fr, en := catalogFile(t, "fr"), catalogFile(t, "en")
	e.post(t, "famille", "/famille/admin/inscriptions/mode", url.Values{"enabled": {"1"}})
	pages := []string{
		e.do("GET", "/famille/admin/inscriptions?ok=enabled&confirm=renew", "en", nil, e.cookies["famille"]).Body.String(),
		e.do("GET", "/famille/admin/inscriptions", "en", nil, e.cookies["famille"]).Body.String(),
	}
	e.ver.resend = errTooSoon()
	e.lang = "en"
	pages = append(pages, e.post(t, "famille", "/famille/admin/inscriptions/7/renvoyer", nil).Body.String())
	e.draw.done = true
	pages = append(pages, e.do("GET", "/famille/admin/inscriptions", "en", nil, e.cookies["famille"]).Body.String())
	for i, body := range pages {
		if i < 2 && !strings.Contains(body, `<html lang="en">`) {
			t.Errorf("page %d : lang=en absent", i)
		}
		text := html.UnescapeString(body)
		for k, v := range fr {
			if strings.Contains(body, k) {
				t.Errorf("page %d : clé brute %q", i, k)
			}
			if v != en[k] && strings.Contains(text, v) {
				t.Errorf("page %d : texte français %q dans le rendu anglais", i, v)
			}
		}
		if m := rawKeyRe.FindString(body); m != "" {
			t.Errorf("page %d : clé brute %q", i, m)
		}
	}
	text := html.UnescapeString(pages[0])
	for _, k := range []string{"registration.admin.title", "registration.admin.renew.confirm", "registration.admin.pending.validate", "registration.admin.limits.save"} {
		if !strings.Contains(text, en[k]) {
			t.Errorf("texte anglais %q absent", en[k])
		}
	}
	if !strings.Contains(text, "Sign-ups") {
		t.Error("libellé de navigation anglais absent")
	}
}

func TestFrenchRender(t *testing.T) {
	e := newEnv(t)
	fr := catalogFile(t, "fr")
	body := html.UnescapeString(e.do("GET", "/famille/admin/inscriptions", "fr", nil, e.cookies["famille"]).Body.String())
	for _, k := range []string{"registration.admin.title", "registration.admin.pending.validate", "registration.admin.pending.resend", "registration.admin.pending.delete", "registration.admin.block.title"} {
		if !strings.Contains(body, fr[k]) {
			t.Errorf("texte français %q absent", fr[k])
		}
	}
	if !strings.Contains(body, "inscrit il y a 3 heures") {
		t.Error("ancienneté absente")
	}
}
