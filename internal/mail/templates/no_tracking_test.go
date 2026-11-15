package templates

import (
	"net/url"
	"regexp"
	"strings"
	"testing"

	"golang.org/x/net/html"
)

// F6.AC12 : aucun courriel ne contient d'image distante, de pixel de suivi ni
// de lien de suivi tiers.
func TestNoTrackingInAnyMail(t *testing.T) {
	cat := testCatalog(t)
	tracking := regexp.MustCompile(`(?i)[?&](utm_[a-z]+|fbclid|gclid|mc_eid|mc_cid|track(ing)?|pixel|open)=`)
	for _, lang := range []string{"fr", "en"} {
		for _, k := range Kinds() {
			d := sampleData(k)
			d.ParticipantLang = lang
			m, out := buildKind(t, cat, k, d)
			_, htm, _ := decodedParts(t, out.Raw)
			z := html.NewTokenizer(strings.NewReader(htm))
			for {
				tt := z.Next()
				if tt == html.ErrorToken {
					break
				}
				if tt != html.StartTagToken && tt != html.SelfClosingTagToken {
					continue
				}
				tok := z.Token()
				switch tok.Data {
				case "img", "link", "script", "iframe", "object", "embed", "video", "audio", "source", "style":
					t.Errorf("%s/%s : balise <%s> interdite", lang, k, tok.Data)
				}
				for _, a := range tok.Attr {
					if a.Key == "src" || a.Key == "background" || strings.HasPrefix(a.Key, "on") {
						t.Errorf("%s/%s : attribut %s", lang, k, a.Key)
					}
					if a.Key == "href" {
						u, err := url.Parse(a.Val)
						if err != nil || u.Host != testHost {
							t.Errorf("%s/%s : lien hors de l'application : %q", lang, k, a.Val)
						}
					}
				}
			}
			for _, part := range []string{m.Text, m.HTML} {
				if tracking.MatchString(part) {
					t.Errorf("%s/%s : paramètre de suivi", lang, k)
				}
				if regexp.MustCompile(`(?i)url\s*\(|data:image|<img`).MatchString(part) {
					t.Errorf("%s/%s : ressource distante ou embarquée", lang, k)
				}
			}
		}
	}
}
