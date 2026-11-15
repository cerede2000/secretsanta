package templates

import (
	"strings"
	"testing"
)

// Render pose Message.Lang avec la langue résolue : le courriel anglais porte
// Content-Language: en et <html lang="en">, le courriel français fr.
func TestRenderSetsMessageLanguage(t *testing.T) {
	cat := testCatalog(t)
	cases := []struct {
		participant, group, want string
	}{
		{"en", "", "en"},
		{"", "en", "en"},
		{"en-GB", "", "en"},
		{"", "", "fr"},
		{"fr", "en", "fr"},
	}
	for _, c := range cases {
		for _, k := range Kinds() {
			d := sampleData(k)
			d.ParticipantLang, d.GroupLang = c.participant, c.group
			m, out := buildKind(t, cat, k, d)
			if m.Lang != c.want {
				t.Fatalf("%s (%q/%q) : Lang = %q, attendu %q", k, c.participant, c.group, m.Lang, c.want)
			}
			if got := parseRaw(t, out.Raw).Header.Get("Content-Language"); got != c.want {
				t.Errorf("%s (%q/%q) : Content-Language = %q, attendu %q", k, c.participant, c.group, got, c.want)
			}
			_, htm, _ := decodedParts(t, out.Raw)
			if !strings.Contains(htm, `<html lang="`+c.want+`">`) {
				t.Errorf("%s (%q/%q) : balise <html lang> attendue %q", k, c.participant, c.group, c.want)
			}
		}
	}
}
