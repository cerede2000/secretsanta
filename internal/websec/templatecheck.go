package websec

import (
	"fmt"
	"regexp"
	"strings"
)

var (
	// Balise <script ...> ... </script> (insensible à la casse, multiligne).
	scriptRe = regexp.MustCompile(`(?is)<script\b([^>]*)>(.*?)</script\s*>`)
	// Balise <script ...> non fermée (comptée à part).
	scriptOpenRe = regexp.MustCompile(`(?i)<script\b`)
	srcAttrRe    = regexp.MustCompile(`(?i)\bsrc\s*=\s*["']?([^"'\s>]+)`)
	typeAttrRe   = regexp.MustCompile(`(?i)\btype\s*=\s*["']?([^"'\s>]+)`)
	// Attribut gestionnaire d'événement (onclick=, onload=…) dans une balise.
	handlerRe = regexp.MustCompile(`(?is)<[a-z][^>]*\s(on[a-z]+)\s*=`)
	// URL javascript: dans un attribut.
	jsURLRe = regexp.MustCompile(`(?i)=\s*["']?\s*javascript:`)
)

// CheckTemplate rapporte les infractions d'un gabarit à la CSP stricte :
// script en ligne (tout <script> sans src, avec contenu, ou ne visant pas un
// module ES de /static/), gestionnaire d'événement en ligne, URL javascript:.
// Une liste vide signifie conforme.
func CheckTemplate(name, content string) []string {
	var problems []string
	matches := scriptRe.FindAllStringSubmatch(content, -1)
	if len(scriptOpenRe.FindAllStringIndex(content, -1)) != len(matches) {
		problems = append(problems, fmt.Sprintf("%s : balise <script> non fermée", name))
	}
	for _, m := range matches {
		attrs, body := m[1], m[2]
		src := srcAttrRe.FindStringSubmatch(attrs)
		switch {
		case src == nil:
			problems = append(problems, fmt.Sprintf("%s : script en ligne", name))
		case strings.TrimSpace(body) != "":
			problems = append(problems, fmt.Sprintf("%s : script avec contenu en ligne", name))
		case !strings.HasPrefix(src[1], "/static/"):
			problems = append(problems, fmt.Sprintf("%s : script hors de /static/ (%s)", name, src[1]))
		default:
			if t := typeAttrRe.FindStringSubmatch(attrs); t == nil || !strings.EqualFold(t[1], "module") {
				problems = append(problems, fmt.Sprintf("%s : script qui n'est pas un module ES", name))
			}
		}
	}
	for _, m := range handlerRe.FindAllStringSubmatch(content, -1) {
		problems = append(problems, fmt.Sprintf("%s : gestionnaire d'événement en ligne (%s)", name, strings.ToLower(m[1])))
	}
	if jsURLRe.MatchString(content) {
		problems = append(problems, fmt.Sprintf("%s : URL javascript:", name))
	}
	return problems
}
