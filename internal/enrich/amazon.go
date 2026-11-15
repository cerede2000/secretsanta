// Package enrich complète en meilleur effort les liens Amazon trouvés par la
// recherche (titre, image, prix) par extraction de la page, sans API payante.
// Toute requête sortante passe par la garde SSRF ; aucune donnée personnelle
// n'est envoyée. Un échec d'extraction conserve la suggestion telle quelle.
package enrich

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"golang.org/x/net/html"

	"secretsanta/internal/ssrf"
)

// Suggestion est la partie d'une suggestion que l'enrichissement complète.
// Les champs vides (ou PriceCents nul) sont inconnus.
type Suggestion struct {
	URL        string
	Title      string
	ImageSrc   string // copie locale (même origine), jamais une adresse externe
	PriceCents int
	Currency   string
}

// Fetcher est la partie de ssrf.Guard utilisée ; *ssrf.Guard la satisfait.
type Fetcher interface {
	FetchPage(ctx context.Context, rawURL string) (*ssrf.Response, error)
	FetchImage(ctx context.Context, rawURL string) (*ssrf.Image, error)
}

// ImageSaver est la partie de ssrf.Store utilisée ; *ssrf.Store la satisfait.
type ImageSaver interface {
	Save(img *ssrf.Image) (ssrf.LocalImage, error)
}

// Amazon enrichit les liens Amazon.
type Amazon struct {
	Fetcher Fetcher
	Images  ImageSaver
	Logger  *slog.Logger
}

// IsAmazonURL dit si l'adresse vise un site Amazon (amazon.<tld>, amzn.to, a.co).
func IsAmazonURL(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") {
		return false
	}
	h := strings.ToLower(u.Hostname())
	switch {
	case h == "amzn.to", h == "amzn.eu", h == "a.co":
		return true
	}
	parts := strings.Split(h, ".")
	for i, p := range parts {
		if p == "amazon" && i < len(parts)-1 && i >= 0 {
			rest := parts[i+1:]
			return len(rest) <= 2
		}
	}
	return false
}

// Enrich renvoie la suggestion complétée. Elle ne renvoie jamais d'erreur :
// en cas d'échec, les données disponibles sont conservées. Les champs déjà
// connus ne sont pas écrasés.
func (a *Amazon) Enrich(ctx context.Context, s Suggestion) Suggestion {
	if a == nil || a.Fetcher == nil || !IsAmazonURL(s.URL) {
		return s
	}
	resp, err := a.Fetcher.FetchPage(ctx, s.URL)
	if err != nil || resp == nil || resp.StatusCode < 200 || resp.StatusCode >= 300 {
		a.logf("page non récupérée")
		return s
	}
	info := Parse(resp.Body, resp.URL)
	if s.Title == "" {
		s.Title = info.Title
	}
	if s.PriceCents == 0 && info.PriceCents > 0 {
		s.PriceCents, s.Currency = info.PriceCents, info.Currency
	}
	if s.ImageSrc == "" && info.ImageURL != "" && a.Images != nil {
		if img, err := a.Fetcher.FetchImage(ctx, info.ImageURL); err == nil {
			if li, err := a.Images.Save(img); err == nil {
				s.ImageSrc = li.Src
			}
		} else {
			a.logf("image non récupérée")
		}
	}
	return s
}

func (a *Amazon) logf(msg string) {
	if a.Logger != nil {
		// Ni adresse ni contenu : rien de personnel ni de secret dans les journaux.
		a.Logger.Debug("enrichissement Amazon : " + msg)
	}
}

// Info est ce que l'analyse d'une page a trouvé.
type Info struct {
	Title      string
	ImageURL   string // absolue
	PriceCents int
	Currency   string
}

// ParsePrice lit un montant (« 12,99 € », « $1,299.00 », "19.9") en centimes.
func ParsePrice(raw string) (int, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0, false
	}
	// Le dernier séparateur est décimal s'il est suivi de 1 ou 2 chiffres.
	m := regexp.MustCompile(`\d[\d.,\s\x{a0}\x{202f}]*\d|\d`).FindString(raw)
	if m == "" {
		return 0, false
	}
	m = strings.Map(func(r rune) rune {
		if r == ' ' || r == ' ' || r == ' ' {
			return -1
		}
		return r
	}, m)
	i := strings.LastIndexAny(m, ".,")
	whole, frac := m, ""
	if i >= 0 {
		after := m[i+1:]
		if len(after) <= 2 {
			whole, frac = m[:i], after
		}
	}
	whole = strings.NewReplacer(".", "", ",", "").Replace(whole)
	w, err := strconv.Atoi(whole)
	if err != nil {
		return 0, false
	}
	for len(frac) < 2 {
		frac += "0"
	}
	f, _ := strconv.Atoi(frac)
	c := w*100 + f
	return c, c > 0
}

// Parse extrait titre, image et prix d'une page produit ; elle ne plante pas
// sur un HTML bancal et renvoie ce qu'elle a pu trouver.
func Parse(body []byte, base *url.URL) Info {
	doc, err := html.Parse(strings.NewReader(string(body)))
	if err != nil {
		return Info{}
	}
	var (
		ogTitle, ogImage, metaPrice, metaCur string
		htmlTitle, productTitle, landing     string
		offscreen                            string
		ld                                   Info
	)
	var walk func(n *html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode {
			switch n.Data {
			case "meta":
				key := attr(n, "property")
				if key == "" {
					key = attr(n, "name")
				}
				c := attr(n, "content")
				switch key {
				case "og:title", "twitter:title":
					if ogTitle == "" {
						ogTitle = c
					}
				case "og:image", "twitter:image":
					if ogImage == "" {
						ogImage = c
					}
				case "product:price:amount", "og:price:amount":
					metaPrice = c
				case "product:price:currency", "og:price:currency":
					metaCur = c
				}
			case "title":
				if htmlTitle == "" {
					htmlTitle = text(n)
				}
			case "span":
				switch attr(n, "id") {
				case "productTitle":
					if productTitle == "" {
						productTitle = text(n)
					}
				}
				if offscreen == "" && hasClass(n, "a-offscreen") {
					offscreen = text(n)
				}
			case "img":
				if attr(n, "id") == "landingImage" && landing == "" {
					landing = firstNonEmpty(attr(n, "data-old-hires"), attr(n, "src"))
				}
			case "script":
				if strings.EqualFold(attr(n, "type"), "application/ld+json") && ld.Title == "" {
					if i := parseLD(text(n)); i.Title != "" || i.PriceCents > 0 || i.ImageURL != "" {
						ld = i
					}
				}
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(doc)

	out := Info{
		Title:    firstNonEmpty(clean(productTitle), clean(ld.Title), clean(ogTitle), cleanPageTitle(htmlTitle)),
		ImageURL: absolute(base, firstNonEmpty(landing, ld.ImageURL, ogImage)),
	}
	switch {
	case ld.PriceCents > 0:
		out.PriceCents, out.Currency = ld.PriceCents, ld.Currency
	default:
		if c, ok := ParsePrice(metaPrice); ok {
			out.PriceCents, out.Currency = c, metaCur
		} else if c, ok := ParsePrice(offscreen); ok {
			out.PriceCents, out.Currency = c, currencyOf(offscreen)
		}
	}
	return out
}

func parseLD(raw string) Info {
	var v any
	if json.Unmarshal([]byte(raw), &v) != nil {
		return Info{}
	}
	var find func(v any) Info
	find = func(v any) Info {
		switch t := v.(type) {
		case []any:
			for _, e := range t {
				if i := find(e); i.Title != "" || i.PriceCents > 0 {
					return i
				}
			}
		case map[string]any:
			if g, ok := t["@graph"]; ok {
				if i := find(g); i.Title != "" || i.PriceCents > 0 {
					return i
				}
			}
			if isType(t["@type"], "Product") {
				var i Info
				i.Title, _ = t["name"].(string)
				switch im := t["image"].(type) {
				case string:
					i.ImageURL = im
				case []any:
					if len(im) > 0 {
						i.ImageURL, _ = im[0].(string)
					}
				case map[string]any:
					i.ImageURL, _ = im["url"].(string)
				}
				i.PriceCents, i.Currency = ldOffer(t["offers"])
				return i
			}
		}
		return Info{}
	}
	return find(v)
}

func ldOffer(v any) (int, string) {
	switch t := v.(type) {
	case []any:
		for _, e := range t {
			if c, cur := ldOffer(e); c > 0 {
				return c, cur
			}
		}
	case map[string]any:
		cur, _ := t["priceCurrency"].(string)
		for _, k := range []string{"price", "lowPrice"} {
			var s string
			switch p := t[k].(type) {
			case string:
				s = p
			case float64:
				s = strconv.FormatFloat(p, 'f', -1, 64)
			}
			if c, ok := ParsePrice(s); ok {
				return c, cur
			}
		}
		if o, ok := t["offers"]; ok {
			return ldOffer(o)
		}
	}
	return 0, ""
}

func isType(v any, want string) bool {
	switch t := v.(type) {
	case string:
		return t == want
	case []any:
		for _, e := range t {
			if s, _ := e.(string); s == want {
				return true
			}
		}
	}
	return false
}

func currencyOf(s string) string {
	switch {
	case strings.Contains(s, "€"):
		return "EUR"
	case strings.Contains(s, "£"):
		return "GBP"
	case strings.Contains(s, "$"):
		return "USD"
	}
	return ""
}

func attr(n *html.Node, k string) string {
	for _, a := range n.Attr {
		if a.Key == k {
			return a.Val
		}
	}
	return ""
}

func hasClass(n *html.Node, c string) bool {
	for _, f := range strings.Fields(attr(n, "class")) {
		if f == c {
			return true
		}
	}
	return false
}

func text(n *html.Node) string {
	var sb strings.Builder
	var w func(*html.Node)
	w = func(n *html.Node) {
		if n.Type == html.TextNode {
			sb.WriteString(n.Data)
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			w(c)
		}
	}
	w(n)
	return sb.String()
}

func clean(s string) string { return strings.Join(strings.Fields(s), " ") }

func cleanPageTitle(s string) string {
	s = clean(s)
	if strings.HasPrefix(strings.ToLower(s), "amazon") && !strings.Contains(s, ":") {
		return ""
	}
	return s
}

func firstNonEmpty(vs ...string) string {
	for _, v := range vs {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

func absolute(base *url.URL, ref string) string {
	if ref == "" {
		return ""
	}
	r, err := url.Parse(ref)
	if err != nil {
		return ""
	}
	if base != nil {
		r = base.ResolveReference(r)
	}
	if r.Scheme != "http" && r.Scheme != "https" {
		return ""
	}
	return r.String()
}
