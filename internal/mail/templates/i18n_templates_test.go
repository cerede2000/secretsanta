package templates

import (
	"regexp"
	"sort"
	"strings"
	"testing"
	"unicode/utf8"

	"secretsanta/internal/mailmsg"
)

var paramRe = regexp.MustCompile(`\{([A-Za-z_][A-Za-z0-9_]*)\}`)

func paramsOf(s string) string {
	seen := map[string]bool{}
	for _, m := range paramRe.FindAllStringSubmatch(s, -1) {
		seen[m[1]] = true
	}
	var out []string
	for k := range seen {
		out = append(out, k)
	}
	sort.Strings(out)
	return strings.Join(out, ",")
}

// F6.AC36 : chaque modèle existe en français et en anglais avec les mêmes
// paramètres, un objet d'au plus 60 caractères sans capitales intégrales ni
// « !!! », et un texte équivalent au HTML.
func TestTemplatesExistInFrenchAndEnglish(t *testing.T) {
	cat := testCatalog(t)
	fr, en := cat.Messages("fr"), cat.Messages("en")
	n := 0
	for k, v := range fr {
		if !strings.HasPrefix(k, prefix) {
			continue
		}
		n++
		w, ok := en[k]
		if !ok {
			t.Errorf("%s : absent en anglais", k)
			continue
		}
		if paramsOf(v) != paramsOf(w) {
			t.Errorf("%s : paramètres fr %q, en %q", k, paramsOf(v), paramsOf(w))
		}
	}
	if n == 0 {
		t.Fatal("aucune clé mail.tpl.*")
	}
	for k := range en {
		if strings.HasPrefix(k, prefix) {
			if _, ok := fr[k]; !ok {
				t.Errorf("%s : absent en français", k)
			}
		}
	}
	for _, k := range Kinds() {
		if _, ok := fr[prefix+string(k)+".subject"]; !ok {
			t.Errorf("%s : objet absent", k)
		}
	}
}

func TestEnglishMailsAreEnglishAndConform(t *testing.T) {
	cat := testCatalog(t)
	for _, k := range Kinds() {
		d := sampleData(k)
		d.ParticipantLang = "en"
		m, _ := buildKind(t, cat, k, d)
		fr := render(t, cat, k, sampleData(k))
		if m.Subject == fr.Subject || m.Text == fr.Text {
			t.Errorf("%s : le courriel anglais est identique au français", k)
		}
		if !strings.HasPrefix(m.Text, "Hello Léa,") {
			t.Errorf("%s : %q", k, m.Text[:20])
		}
		if utf8.RuneCountInString(m.Subject) > 60 || strings.Contains(m.Subject, "!!!") {
			t.Errorf("%s : objet %q", k, m.Subject)
		}
		if err := mailmsg.LintSubject(m.Subject); err != nil {
			t.Errorf("%s : %v", k, err)
		}
		if err := mailmsg.ParityCheck(m.Text, m.HTML); err != nil {
			t.Errorf("%s : texte et HTML non équivalents : %v", k, err)
		}
	}
}

func TestPluralIdeas(t *testing.T) {
	cat := testCatalog(t)
	for lang, want := range map[string][2]string{"fr": {"1 idée.", "3 idées."}, "en": {"1 idea.", "3 ideas."}} {
		for i, n := range []int{1, 3} {
			d := sampleData(LetterUpdated)
			d.ParticipantLang, d.Ideas = lang, n
			if m := render(t, cat, LetterUpdated, d); !strings.Contains(m.Text, want[i]) {
				t.Errorf("%s %d : %q absent", lang, n, want[i])
			}
		}
	}
}
