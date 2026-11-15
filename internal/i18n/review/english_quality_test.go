package review

import (
	"strings"
	"testing"
)

// frenchToolWords est la liste documentée des mots-outils français courants
// qui n'ont aucune raison d'apparaître dans une valeur anglaise. Les mots
// ambigus avec l'anglais (a, on, as, son, but, he, me, in) en sont exclus.
var frenchToolWords = map[string]bool{
	"le": true, "la": true, "les": true, "des": true, "du": true, "un": true,
	"une": true, "et": true, "ou": true, "est": true, "sont": true, "pas": true,
	"avec": true, "dans": true, "sur": true, "ton": true, "ta": true, "tes": true,
	"votre": true, "vos": true, "pour": true, "vous": true, "nous": true,
	"ce": true, "cette": true, "ces": true, "qui": true, "que": true, "mais": true,
	"sans": true, "par": true, "aux": true, "au": true, "ne": true, "se": true,
	"il": true, "elle": true, "ils": true, "elles": true, "cadeau": true,
	"cadeaux": true, "tirage": true, "lettre": true, "bienvenue": true,
	"échange": true, "bonjour": true, "merci": true, "mon": true, "mes": true,
}

func TestEnglishHasNoFrenchToolWords(t *testing.T) {
	cat := load(t)
	en := cat.Messages("en")
	if len(en) == 0 {
		t.Fatal("no English messages")
	}
	for _, k := range sortedKeys(en) {
		for _, w := range words(en[k]) {
			if frenchToolWords[w] {
				t.Errorf("%s: French tool word %q in %q", k, w, en[k])
			}
		}
	}
}

func TestEnglishLongValuesDifferFromFrench(t *testing.T) {
	cat := load(t)
	en, fr := cat.Messages("en"), cat.Messages("fr")
	for _, k := range sortedKeys(en) {
		if len(words(en[k])) <= 3 {
			continue
		}
		if f, ok := fr[k]; ok && strings.TrimSpace(f) == strings.TrimSpace(en[k]) {
			t.Errorf("%s: English identical to French: %q", k, en[k])
		}
	}
}

func TestEnglishPluralsComplete(t *testing.T) {
	cat := load(t)
	en := cat.Messages("en")
	for _, k := range sortedKeys(en) {
		for _, suf := range []string{".one", ".other"} {
			if base, ok := strings.CutSuffix(k, suf); ok {
				other := ".other"
				if suf == ".other" {
					other = ".one"
				}
				if _, ok := en[base+other]; !ok {
					t.Errorf("%s: missing %s%s", k, base, other)
				}
			}
		}
	}
}
