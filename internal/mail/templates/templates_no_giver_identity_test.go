package templates

import (
	"reflect"
	"regexp"
	"strings"
	"testing"
)

// F6.AC11 : aucun modèle ni charge utile ne contient l'identité du tireur ni
// le nom d'une autre personne que le destinataire.

// Les données d'un modèle ne peuvent porter aucune autre personne.
func TestDataCarriesNoOtherPerson(t *testing.T) {
	allowed := map[string]bool{
		"To": true, "FirstName": true, "ParticipantLang": true, "GroupLang": true, "GroupName": true,
		"AppName": true, "Link": true, "LetterLink": true, "Code": true, "BudgetCents": true,
		"Ideas": true, "UnsubscribeURL": true, "UnsubscribeMailto": true,
	}
	rt := reflect.TypeOf(Data{})
	for i := 0; i < rt.NumField(); i++ {
		if n := rt.Field(i).Name; !allowed[n] {
			t.Errorf("champ %q : une charge utile de modèle ne doit pas porter d'identité autre que le destinataire", n)
		}
	}
}

// Les catalogues n'emploient que les paramètres connus (aucun {giver},
// {target}, {other}...).
func TestCatalogParametersAreRecipientOnly(t *testing.T) {
	cat := testCatalog(t)
	known := map[string]bool{"name": true, "group": true, "app": true, "code": true, "budget": true, "n": true}
	re := regexp.MustCompile(`\{([A-Za-z_][A-Za-z0-9_]*)\}`)
	for _, lang := range []string{"fr", "en"} {
		for k, v := range cat.Messages(lang) {
			if !strings.HasPrefix(k, "mail.tpl.") {
				continue
			}
			for _, m := range re.FindAllStringSubmatch(v, -1) {
				if !known[m[1]] {
					t.Errorf("%s %s : paramètre {%s} inconnu", lang, k, m[1])
				}
			}
		}
	}
}

// Rendu avec un prénom sentinelle : seul le destinataire apparaît ; aucune
// mention du tireur, d'une cible ou du mot « tireur » dans le rendu.
func TestRenderedMailNamesOnlyTheRecipient(t *testing.T) {
	cat := testCatalog(t)
	others := []string{"Hugo", "Camille", "Zoé", "Nathan"}
	banned := regexp.MustCompile(`(?i)\b(tireur|giver|drawn|target|cible)\b|offre à|offres à|offre un cadeau à`)
	for _, lang := range []string{"fr", "en"} {
		for _, k := range Kinds() {
			d := sampleData(k)
			d.ParticipantLang = lang
			m := render(t, cat, k, d)
			all := m.Subject + "\n" + m.Text + "\n" + m.HTML
			for _, o := range others {
				if strings.Contains(all, o) {
					t.Errorf("%s/%s : nom %q", lang, k, o)
				}
			}
			if banned.MatchString(all) {
				t.Errorf("%s/%s : formulation désignant une personne : %q", lang, k, banned.FindString(all))
			}
			if strings.Contains(m.Subject, "Léa") || strings.Contains(m.Subject, "Martin") {
				t.Errorf("%s/%s : l'objet nomme une personne : %q", lang, k, m.Subject)
			}
		}
	}
}
