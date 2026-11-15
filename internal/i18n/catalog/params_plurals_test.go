package catalog

import (
	"testing"
	"testing/fstest"
)

func pluralFS() fstest.MapFS {
	return fstest.MapFS{
		"static/i18n/core/fr.json": {Data: []byte(`{"c.items.one":"{n} cadeau","c.items.other":"{n} cadeaux","c.who":"Salut {name} et {name}","c.brace":"{ pas {un param} {1x} }"}`)},
		"static/i18n/core/en.json": {Data: []byte(`{"c.items.one":"{n} gift","c.items.other":"{n} gifts"}`)},
		"static/i18n/core/de.json": {Data: []byte(`{"c.items.one":"{n} Geschenk","c.items.other":"{n} Geschenke"}`)},
	}
}

func TestParams(t *testing.T) {
	lg, _ := testLogger()
	c, err := Load(pluralFS(), Options{Logger: lg})
	if err != nil {
		t.Fatal(err)
	}
	if got := c.T("fr", "c.who", Params{"name": "Léa", "extra": 1}); got != "Salut Léa et Léa" {
		t.Errorf("%q", got)
	}
	if got := c.T("fr", "c.who", Params{"name": "{name}"}); got != "Salut {name} et {name}" {
		t.Errorf("valeur non réinterprétée : %q", got)
	}
	if got := c.T("fr", "c.who", Params{"name": "<b>x</b>"}); got != "Salut <b>x</b> et <b>x</b>" {
		t.Errorf("texte brut attendu : %q", got)
	}
	if got := c.T("fr", "c.who", nil); got != "Salut {name} et {name}" {
		t.Errorf("paramètre absent : %q", got)
	}
	if got := c.T("fr", "c.brace", Params{"un param": 1}); got != "{ pas {un param} {1x} }" {
		t.Errorf("accolades : %q", got)
	}
}

func TestPlurals(t *testing.T) {
	lg, _ := testLogger()
	c, _ := Load(pluralFS(), Options{Logger: lg})
	cases := []struct {
		lang string
		n    int
		want string
	}{
		{"fr", 0, "0 cadeau"}, {"fr", 1, "1 cadeau"}, {"fr", 2, "2 cadeaux"},
		{"en", 0, "0 gifts"}, {"en", 1, "1 gift"}, {"en", 2, "2 gifts"},
		{"de", 0, "0 Geschenke"}, {"de", 1, "1 Geschenk"}, {"de", 5, "5 Geschenke"},
		{"en", -1, "-1 gift"},
	}
	for _, tc := range cases {
		if got := c.TN(tc.lang, "c.items", tc.n, nil); got != tc.want {
			t.Errorf("%s %d : %q, attendu %q", tc.lang, tc.n, got, tc.want)
		}
	}
}

func TestPluralFallsBackToDefaultLanguageRule(t *testing.T) {
	fsys := pluralFS()
	delete(fsys, "static/i18n/core/en.json")
	fsys["static/i18n/core/en.json"] = &fstest.MapFile{Data: []byte(`{"c.other":"x"}`)}
	lg, _ := testLogger()
	c, _ := Load(fsys, Options{Logger: lg})
	if got := c.TN("en", "c.items", 0, nil); got != "0 cadeau" {
		t.Errorf("règle du français pour le texte français : %q", got)
	}
}

func TestPluralCategory(t *testing.T) {
	if PluralCategory("fr-CA", 0) != "one" || PluralCategory("en", 0) != "other" || PluralCategory("en", 1) != "one" {
		t.Error("règles de pluriel")
	}
}
