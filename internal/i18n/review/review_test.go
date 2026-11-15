// Package review contient les tests de qualité des catalogues anglais.
package review

import (
	"io"
	"log/slog"
	"regexp"
	"sort"
	"strings"
	"testing"

	"secretsanta/internal/i18n/catalog"
	"secretsanta/web"
)

func load(t *testing.T) *catalog.Catalog {
	t.Helper()
	cat, err := catalog.Load(web.I18n, catalog.Options{
		DefaultLang: "fr",
		Logger:      slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	if err != nil {
		t.Fatal(err)
	}
	return cat
}

var wordRe = regexp.MustCompile(`[\p{L}']+`)

func words(s string) []string {
	// retire les paramètres {nom}
	s = regexp.MustCompile(`\{[^}]*\}`).ReplaceAllString(s, " ")
	return wordRe.FindAllString(strings.ToLower(s), -1)
}

func sortedKeys(m map[string]string) []string {
	k := make([]string, 0, len(m))
	for key := range m {
		k = append(k, key)
	}
	sort.Strings(k)
	return k
}
