package guard

import (
	"strings"
	"testing"
)

// sameInBothLanguages est la liste d'exceptions : clés dont la valeur anglaise
// de plus de trois mots est légitimement identique à la française (par exemple
// un exemple d'adresse ou un nom propre). Vide aujourd'hui ; toute entrée doit
// être justifiée par un commentaire.
var sameInBothLanguages = map[string]string{}

func TestEnglishDiffersFromFrench(t *testing.T) {
	cats := loadCatalogs(t)
	fr, en := cats.lang("fr"), cats.lang("en")
	for _, k := range sortedKeys(en) {
		f, ok := fr[k]
		if !ok {
			continue // signalé par la parité
		}
		if len(strings.Fields(en[k])) <= 3 {
			continue
		}
		if en[k] == f {
			if _, ok := sameInBothLanguages[k]; !ok {
				t.Errorf("la valeur anglaise de %q est identique à la française (%q) : traduire ou justifier dans sameInBothLanguages", k, f)
			}
		}
	}
	for k := range sameInBothLanguages {
		if en[k] != fr[k] || len(strings.Fields(en[k])) <= 3 {
			t.Errorf("sameInBothLanguages[%q] est inutile : retirer l'exception", k)
		}
	}
}
