package templates

import (
	"strings"
	"testing"
)

// F6.AC14 : invitation et tirage prêt n'affichent aucune mention de budget
// quand il est vide, et l'affichent quand il est défini.
func TestOptionalBudget(t *testing.T) {
	cat := testCatalog(t)
	for _, lang := range []string{"fr", "en"} {
		word := map[string]string{"fr": "udget", "en": "udget"}[lang]
		for _, k := range []Kind{Invitation, ReadyOne, ReadyTwo, ReadyReception} {
			d := sampleData(k)
			d.ParticipantLang = lang

			d.BudgetCents = nil
			m := render(t, cat, k, d)
			for _, part := range []string{m.Subject, m.Text, m.HTML} {
				if strings.Contains(part, word) || strings.Contains(part, "€") {
					t.Errorf("%s/%s : mention de budget alors qu'il est vide :\n%s", lang, k, part)
				}
			}
			buildKind(t, cat, k, d)

			d.BudgetCents = budget(2550)
			m = render(t, cat, k, d)
			want := FormatBudget(lang, 2550)
			for _, part := range []string{m.Text, m.HTML} {
				if !strings.Contains(part, want) {
					t.Errorf("%s/%s : budget %q absent :\n%s", lang, k, want, part)
				}
			}
			buildKind(t, cat, k, d)
		}
	}
}
