package templates

import (
	"mime"
	"strings"
	"testing"
	"unicode/utf8"

	"secretsanta/internal/mailmsg"
)

// F6.AC28 : objet encodé RFC 2047, au plus 60 caractères, sans mot en
// majuscules ni « !!! », sans autre personne que le destinataire.
func TestSubjects(t *testing.T) {
	cat := testCatalog(t)
	for _, lang := range []string{"fr", "en"} {
		for _, k := range Kinds() {
			d := sampleData(k)
			d.ParticipantLang = lang
			m, out := buildKind(t, cat, k, d)
			if n := utf8.RuneCountInString(m.Subject); n == 0 || n > 60 {
				t.Errorf("%s/%s : objet de %d caractères", lang, k, n)
			}
			if err := mailmsg.LintSubject(m.Subject); err != nil {
				t.Errorf("%s/%s : %v", lang, k, err)
			}
			for _, bad := range []string{"Hugo", "Camille", "Léa", "Martin"} {
				if strings.Contains(m.Subject, bad) {
					t.Errorf("%s/%s : l'objet nomme %q", lang, k, bad)
				}
			}
			raw := parseRaw(t, out.Raw).Header.Get("Subject")
			dec, err := new(mime.WordDecoder).DecodeHeader(raw)
			if err != nil || dec != m.Subject {
				t.Errorf("%s/%s : objet décodé %q (%v), attendu %q", lang, k, dec, err, m.Subject)
			}
			if lang == "fr" && strings.ContainsAny(m.Subject, "éèêàôîç") && !strings.Contains(raw, "=?UTF-8?") {
				t.Errorf("%s : objet non encodé RFC 2047 : %q", k, raw)
			}
		}
	}
}
