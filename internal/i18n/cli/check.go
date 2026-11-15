package cli

import (
	"fmt"
	"os"
	"regexp"
	"sort"
	"strings"

	"secretsanta/internal/i18n/catalog"
)

// Issue est un écart relevé par la vérification.
type Issue struct {
	Kind    string // json, manquante, en_trop, parametres, pluriel, a_traduire
	Key     string
	Message string
}

func (i Issue) String() string {
	if i.Key == "" {
		return fmt.Sprintf("[%s] %s", i.Kind, i.Message)
	}
	return fmt.Sprintf("[%s] %s : %s", i.Kind, i.Key, i.Message)
}

var paramRe = regexp.MustCompile(`\{([A-Za-z_][A-Za-z0-9_]*)\}`)

func paramsOf(s string) string {
	set := map[string]bool{}
	for _, m := range paramRe.FindAllStringSubmatch(s, -1) {
		set[m[1]] = true
	}
	names := make([]string, 0, len(set))
	for n := range set {
		names = append(names, n)
	}
	sort.Strings(names)
	return strings.Join(names, ",")
}

// CheckFile contrôle un fichier plat par rapport à la langue par défaut.
func CheckFile(cat *catalog.Catalog, file string) ([]Issue, error) {
	data, err := os.ReadFile(file)
	if err != nil {
		return nil, err
	}
	msgs, _, ref, err := catalog.ParseFlat(data)
	if err != nil {
		return []Issue{{Kind: "json", Message: err.Error()}}, nil
	}
	return compare(cat.Messages(cat.DefaultLang()), msgs, ref), nil
}

// CheckLang contrôle une langue chargée par rapport à la langue par défaut.
func CheckLang(cat *catalog.Catalog, lang string) ([]Issue, error) {
	msgs := cat.Messages(lang)
	if msgs == nil {
		return nil, fmt.Errorf("langue inconnue : %q", lang)
	}
	if lang == cat.DefaultLang() {
		return nil, nil
	}
	return compare(cat.Messages(cat.DefaultLang()), msgs, nil), nil
}

// compare relève les clés manquantes et en trop, les paramètres incohérents,
// les pluriels incomplets et, si une référence est fournie, les valeurs encore
// à traduire (vides ou identiques à la référence).
func compare(def, msgs, ref map[string]string) []Issue {
	var out []Issue
	for _, k := range sortedKeys(def) {
		v, ok := msgs[k]
		if !ok {
			out = append(out, Issue{"manquante", k, "clé absente de la traduction"})
			continue
		}
		if v == "" {
			out = append(out, Issue{"a_traduire", k, "valeur vide"})
			continue
		}
		if want, got := paramsOf(def[k]), paramsOf(v); want != got {
			out = append(out, Issue{"parametres", k, fmt.Sprintf("paramètres {%s} au lieu de {%s}", got, want)})
		}
		if r, ok := ref[k]; ok && r == v {
			out = append(out, Issue{"a_traduire", k, "valeur identique à la référence"})
		}
	}
	for _, k := range sortedKeys(msgs) {
		if _, ok := def[k]; !ok {
			out = append(out, Issue{"en_trop", k, "clé inconnue de la langue par défaut"})
		}
	}
	for _, k := range sortedKeys(msgs) {
		switch {
		case strings.HasSuffix(k, ".one"):
			if _, ok := msgs[strings.TrimSuffix(k, ".one")+".other"]; !ok {
				out = append(out, Issue{"pluriel", k, "forme .other manquante"})
			}
		case strings.HasSuffix(k, ".other"):
			if _, ok := msgs[strings.TrimSuffix(k, ".other")+".one"]; !ok {
				out = append(out, Issue{"pluriel", k, "forme .one manquante"})
			}
		}
	}
	return out
}

func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
