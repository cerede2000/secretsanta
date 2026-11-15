package templates

import (
	"net/url"
	"regexp"
	"strings"
	"testing"
)

// F6.AC26 : multipart/alternative, texte et HTML équivalents, UTF-8 en
// quoted-printable, lignes d'au plus 76 caractères, liens https vers l'hôte de
// PUBLIC_URL, aucun raccourcisseur.
func TestMimeStructure(t *testing.T) {
	cat := testCatalog(t)
	linkRe := regexp.MustCompile(`https?://[^\s"<>]+`)
	for _, lang := range []string{"fr", "en"} {
		for _, k := range Kinds() {
			d := sampleData(k)
			d.ParticipantLang = lang
			_, out := buildKind(t, cat, k, d)
			msg := parseRaw(t, out.Raw)
			if ct := msg.Header.Get("Content-Type"); !strings.HasPrefix(ct, "multipart/alternative") {
				t.Errorf("%s/%s : Content-Type %q", lang, k, ct)
			}
			text, htm, bodies := decodedParts(t, out.Raw)
			if len(bodies) != 2 || text == "" || htm == "" {
				t.Fatalf("%s/%s : deux parties attendues", lang, k)
			}
			// Corps des parties (les en-têtes contenant une URL non coupable
			// relèvent du constructeur de F6b).
			for _, body := range bodies {
				for _, line := range strings.Split(body, "\r\n") {
					if len(line) > 76 {
						t.Errorf("%s/%s : ligne de %d caractères", lang, k, len(line))
					}
				}
			}
			if !strings.Contains(string(out.Raw), "Content-Transfer-Encoding: quoted-printable") ||
				!strings.Contains(string(out.Raw), "charset=UTF-8") {
				t.Errorf("%s/%s : UTF-8 quoted-printable attendu", lang, k)
			}
			tl := linkRe.FindAllString(text, -1)
			hl := linkRe.FindAllString(htm, -1)
			if len(tl) == 0 && k != BudgetUpdated {
				t.Errorf("%s/%s : aucun lien", lang, k)
			}
			for _, l := range append(tl, hl...) {
				u, err := url.Parse(strings.TrimRight(l, ".,"))
				if err != nil || u.Scheme != "https" || u.Host != testHost {
					t.Errorf("%s/%s : lien %q", lang, k, l)
				}
			}
			for _, l := range tl {
				if !strings.Contains(htm, l) {
					t.Errorf("%s/%s : lien %q du texte absent du HTML", lang, k, l)
				}
			}
		}
	}
}
