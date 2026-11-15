package web

import (
	"html/template"
	"strings"
	"testing"
)

func render(t *testing.T, src, lang string) string {
	t.Helper()
	tm := template.Must(template.New("p").Funcs(FuncsPlaceholder()).Parse(src))
	tm = template.Must(tm.Clone())
	tm.Funcs(Funcs(testCatalog(t), reqLang("GET", "/", lang)))
	var b strings.Builder
	if err := tm.Execute(&b, nil); err != nil {
		t.Fatalf("le rendu ne doit jamais échouer : %v", err)
	}
	return b.String()
}

func TestTAndTNUseRequestLanguage(t *testing.T) {
	if got := render(t, `{{t "core.hello" "name" "Zoé"}}`, "en"); got != "Hello Zoé" {
		t.Fatal(got)
	}
	if got := render(t, `{{t "core.hello" "name" "Zoé"}}`, "fr"); got != "Bonjour Zoé" {
		t.Fatal(got)
	}
	if got := render(t, `{{tn "core.items" 0}}|{{tn "core.items" 1}}|{{tn "core.items" 3}}`, "fr"); got != "0 cadeau|1 cadeau|3 cadeaux" {
		t.Fatal(got)
	}
	if got := render(t, `{{tn "core.items" 0}}|{{tn "core.items" 1}}`, "en"); got != "0 gifts|1 gift" {
		t.Fatal(got)
	}
}

func TestParametersAreEscaped(t *testing.T) {
	evil := `<script>alert(1)</script>`
	for _, src := range []string{`{{t "core.hello" "name" .}}`, `{{tn "core.items" 2 "n" .}}`} {
		tm := template.Must(template.New("p").Funcs(FuncsPlaceholder()).Parse(src))
		tm = template.Must(tm.Clone())
		tm.Funcs(Funcs(testCatalog(t), reqLang("GET", "/", "en")))
		var b strings.Builder
		if err := tm.Execute(&b, evil); err != nil {
			t.Fatal(err)
		}
		if strings.Contains(b.String(), "<script") || !strings.Contains(b.String(), "&lt;script&gt;") {
			t.Fatalf("paramètre non inerte : %q", b.String())
		}
	}
	// Dans un attribut aussi.
	got := render(t, `<a title="{{t "core.hello" "name" "\" onmouseover=\"x"}}">`, "en")
	if strings.Contains(got, `" onmouseover="`) {
		t.Fatalf("sortie d'attribut : %q", got)
	}
}

func TestNeverFailsTheRender(t *testing.T) {
	cases := map[string]string{
		`{{t "absent.cle"}}`:            "absent.cle",
		`{{t "core.hello"}}`:            "Bonjour {name}",
		`{{t "core.hello" "name"}}`:     "Bonjour {name}",
		`{{t "core.hello" 42 "x"}}`:     "Bonjour {name}",
		`{{tn "absent.cle" 2}}`:         "absent.cle",
		`{{tn "core.items" "abc"}}`:     "0 cadeau",
		`{{tn "core.items" 2.0}}`:       "2 cadeaux",
		`{{tn "core.items" 5 "n"}}`:     "5 cadeaux",
		`{{t "core.hello" "name" nil}}`: "Bonjour &lt;nil&gt;",
	}
	for src, want := range cases {
		if got := render(t, src, "fr"); got != want {
			t.Errorf("%s : %q, attendu %q", src, got, want)
		}
	}
	// Langue inconnue : repli sur la langue par défaut.
	if got := render(t, `{{t "core.hello" "name" "A"}}`, "xx"); got != "Bonjour A" {
		t.Fatal(got)
	}
}
