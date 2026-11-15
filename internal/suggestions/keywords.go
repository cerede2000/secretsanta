package suggestions

import (
	"crypto/sha256"
	"encoding/hex"
	"regexp"
	"sort"
	"strings"
	"unicode"

	"golang.org/x/text/unicode/norm"
)

// MaxKeywords est le nombre maximal de mots-clés envoyés aux sources.
const MaxKeywords = 4

var (
	emailRe = regexp.MustCompile(`[^\s<>"',;()]*@[^\s<>"',;()]*`)
	urlRe   = regexp.MustCompile(`(?i)\b(?:https?://|www\.)\S+`)
)

// stop : mots vides fréquents (français et anglais) sans valeur de recherche.
var stop = map[string]bool{}

func init() {
	for _, w := range strings.Fields(`les des une aux avec pour dans sur sous par que qui est sont pas plus tres bien
		mon ton son nos vos leurs mes tes ses ces cet cette ceci cela aussi mais donc car ainsi comme fait faire
		the and for with that this from have has are was were not you your our their its but also like love want
		idee idees cadeau cadeaux noel envie voudrais aimerais bonjour merci`) {
		stop[w] = true
	}
}

// fold met en minuscules sans accents.
func fold(s string) string {
	var b strings.Builder
	for _, r := range norm.NFD.String(strings.ToLower(s)) {
		if unicode.Is(unicode.Mn, r) {
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

func words(s string) []string {
	return strings.FieldsFunc(fold(s), func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) })
}

// Keywords tire les mots-clés de cadeau des idées d'une lettre. Sont écartés :
// tout motif d'adresse courriel, les adresses web, les jetons égaux à un nom ou
// à un prénom (ou à l'un de leurs mots) de la liste donnée, les chiffres
// seuls, les mots vides. L'ordre d'apparition est gardé, sans doublon.
func Keywords(texts []string, names []string) []string {
	banned := map[string]bool{}
	for _, n := range names {
		for _, w := range words(n) {
			banned[w] = true
		}
	}
	seen := map[string]bool{}
	var out []string
	for _, t := range texts {
		t = emailRe.ReplaceAllString(t, " ")
		t = urlRe.ReplaceAllString(t, " ")
		for _, w := range words(t) {
			if len([]rune(w)) < 3 || stop[w] || banned[w] || seen[w] || allDigits(w) {
				continue
			}
			seen[w] = true
			out = append(out, w)
			if len(out) == MaxKeywords {
				return out
			}
		}
	}
	return out
}

func allDigits(s string) bool {
	for _, r := range s {
		if !unicode.IsDigit(r) {
			return false
		}
	}
	return true
}

// keywordsKey est l'empreinte stable d'un jeu de mots-clés : la base ne garde
// jamais les mots de la lettre.
func keywordsKey(kw []string) string {
	c := append([]string(nil), kw...)
	sort.Strings(c)
	sum := sha256.Sum256([]byte(strings.Join(c, " ")))
	return hex.EncodeToString(sum[:16])
}
