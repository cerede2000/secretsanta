package templates

import (
	"strings"
	"testing"
)

// F6.AC30 : trois variantes de « tirage prêt », sans nom de cible ; la
// variante reception_seule invite à écrire sa lettre sans lien de découverte.
func TestReadyVariants(t *testing.T) {
	cat := testCatalog(t)
	if ReadyKind(false, 1) != ReadyOne || ReadyKind(false, 2) != ReadyTwo || ReadyKind(true, 2) != ReadyReception || ReadyKind(true, 0) != ReadyReception {
		t.Fatalf("choix de variante incorrect")
	}
	subjects := map[string]bool{}
	for _, k := range []Kind{ReadyOne, ReadyTwo, ReadyReception} {
		d := sampleData(k)
		m, _ := buildKind(t, cat, k, d)
		subjects[m.Subject] = true
		for _, bad := range []string{"Hugo", "Camille", "Nathan", "Zoé"} {
			if strings.Contains(m.Subject+m.Text+m.HTML, bad) {
				t.Errorf("%s : nom de cible %q", k, bad)
			}
		}
	}
	if len(subjects) != 3 {
		t.Errorf("les trois variantes doivent avoir trois objets distincts : %v", subjects)
	}
	two := render(t, cat, ReadyTwo, sampleData(ReadyTwo))
	if !strings.Contains(two.Subject, "tes deux cadeaux") {
		t.Errorf("objet deux cibles : %q", two.Subject)
	}
	one := render(t, cat, ReadyOne, sampleData(ReadyOne))
	if strings.Contains(one.Text, "deux cadeaux") {
		t.Errorf("la variante une cible parle de deux cadeaux")
	}

	rd := sampleData(ReadyReception)
	m := render(t, cat, ReadyReception, rd)
	if !strings.Contains(m.Text, "tu recevras un cadeau") || !strings.Contains(m.Text, "lettre au Père Noël") {
		t.Errorf("reception_seule :\n%s", m.Text)
	}
	if strings.Contains(m.Text+m.HTML, personalLink) {
		t.Errorf("reception_seule contient le lien de découverte")
	}
	// Même si le lien de découverte est fourni, il n'est jamais écrit.
	rd.Link = personalLink
	m = render(t, cat, ReadyReception, rd)
	if strings.Contains(m.Text+m.HTML, personalLink) {
		t.Errorf("reception_seule contient le lien de découverte fourni")
	}
	// Sans lien de lettre, aucun lien ni phrase orpheline.
	rd.LetterLink = ""
	m = render(t, cat, ReadyReception, rd)
	if strings.Contains(m.Text, "https://") && !strings.Contains(m.Text, "\n--\n") {
		t.Errorf("lien inattendu")
	}
	if strings.Contains(m.Text[:strings.Index(m.Text, "\n--\n")], "https://") {
		t.Errorf("aucun lien attendu dans le corps sans lien de lettre")
	}
	buildKind(t, cat, ReadyReception, rd)
}
