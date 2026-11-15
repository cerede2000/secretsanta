package mailmsg

import (
	"fmt"
	"net/url"
	"slices"
	"strings"
	"unicode"

	"golang.org/x/net/html"
	"golang.org/x/text/unicode/norm"
)

// ParityError détaille les écarts entre la version texte et la version HTML.
// Les liens https sont comparés tels quels, les mailto et adresses courriel
// par leur adresse en minuscules (préfixe « mailto: »), les informations mot
// à mot (lettres et chiffres, sans casse, normalisés NFC). Le texte
// alternatif des images n'est pas une information comparée.
type ParityError struct {
	LinksMissingInText []string
	LinksMissingInHTML []string
	WordsMissingInText []string
	WordsMissingInHTML []string
}

func (e *ParityError) Error() string {
	var parts []string
	add := func(label string, items []string) {
		if len(items) > 0 {
			parts = append(parts, fmt.Sprintf("%s : %s", label, strings.Join(items, ", ")))
		}
	}
	add("liens du HTML absents du texte", e.LinksMissingInText)
	add("liens du texte absents du HTML", e.LinksMissingInHTML)
	add("informations du HTML absentes du texte", e.WordsMissingInText)
	add("informations du texte absentes du HTML", e.WordsMissingInHTML)
	return "Le texte et le HTML du courriel ne sont pas équivalents (" + strings.Join(parts, " ; ") + ")."
}

// Unwrap rattache l'écart à la catégorie ErrParity.
func (e *ParityError) Unwrap() error { return ErrParity }

// ParityCheck vérifie que tout lien et toute information du HTML se
// retrouvent dans le texte, et réciproquement. Elle renvoie nil ou un
// *ParityError (errors.Is(err, ErrParity)).
func ParityCheck(text, htmlSrc string) error {
	visible, hrefs := htmlContent(htmlSrc)
	tLinks, tWords := contentOf(text)
	hLinks, hWords := contentOf(visible)
	for _, h := range hrefs {
		hLinks[normalizeLink(h)] = true
	}
	e := &ParityError{
		LinksMissingInText: missing(hLinks, tLinks),
		LinksMissingInHTML: missing(tLinks, hLinks),
		WordsMissingInText: missing(hWords, tWords),
		WordsMissingInHTML: missing(tWords, hWords),
	}
	if len(e.LinksMissingInText)+len(e.LinksMissingInHTML)+len(e.WordsMissingInText)+len(e.WordsMissingInHTML) == 0 {
		return nil
	}
	return e
}

func missing(from, in map[string]bool) []string {
	var out []string
	for k := range from {
		if !in[k] {
			out = append(out, k)
		}
	}
	slices.Sort(out)
	return out
}

// normalizeLink ramène un mailto ou une adresse à « mailto:adresse » en
// minuscules, sans paramètres ; laisse les autres liens tels quels.
func normalizeLink(l string) string {
	if strings.HasPrefix(strings.ToLower(l), "mailto:") {
		addr := l[len("mailto:"):]
		if i := strings.IndexByte(addr, '?'); i >= 0 {
			addr = addr[:i]
		}
		if u, err := url.PathUnescape(addr); err == nil {
			addr = u
		}
		return "mailto:" + strings.ToLower(addr)
	}
	if textEmail.MatchString(l) && textEmail.FindString(l) == l {
		return "mailto:" + strings.ToLower(l)
	}
	return l
}

// contentOf extrait les liens et les mots d'un texte brut.
func contentOf(s string) (links, words map[string]bool) {
	links, words = map[string]bool{}, map[string]bool{}
	s = norm.NFC.String(s)
	for _, l := range linksInText(s) {
		links[normalizeLink(l)] = true
	}
	s = textURL.ReplaceAllString(s, " ")
	for _, m := range textEmail.FindAllString(s, -1) {
		links[normalizeLink(m)] = true
	}
	s = textEmail.ReplaceAllString(s, " ")
	for _, w := range strings.FieldsFunc(s, func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) }) {
		words[strings.ToLower(w)] = true
	}
	return links, words
}

// htmlContent renvoie le texte visible d'un HTML (hors head, title, style,
// script) et les liens href de ses ancres.
func htmlContent(src string) (string, []string) {
	z := html.NewTokenizer(strings.NewReader(src))
	var b strings.Builder
	var hrefs []string
	hidden := 0
	for {
		switch z.Next() {
		case html.ErrorToken:
			return b.String(), hrefs
		case html.TextToken:
			if hidden == 0 {
				b.Write(z.Text())
			}
		case html.StartTagToken, html.SelfClosingTagToken:
			tok := z.Token()
			switch tok.Data {
			case "head", "title", "style", "script":
				if tok.Type == html.StartTagToken {
					hidden++
				}
			}
			if blockElements[tok.Data] {
				b.WriteString(" ")
			}
			if tok.Data == "a" || tok.Data == "area" {
				for _, a := range tok.Attr {
					if a.Key == "href" {
						hrefs = append(hrefs, a.Val)
					}
				}
			}
		case html.EndTagToken:
			tok := z.Token()
			switch tok.Data {
			case "head", "title", "style", "script":
				if hidden > 0 {
					hidden--
				}
			}
			if blockElements[tok.Data] {
				b.WriteString(" ")
			}
		}
	}
}

// blockElements séparent visuellement leur contenu de ce qui les entoure.
var blockElements = map[string]bool{
	"br": true, "p": true, "div": true, "li": true, "ul": true, "ol": true,
	"tr": true, "td": true, "th": true, "table": true, "hr": true, "img": true,
	"h1": true, "h2": true, "h3": true, "h4": true, "h5": true, "h6": true,
	"blockquote": true, "pre": true, "section": true, "header": true,
	"footer": true, "body": true, "html": true,
}
