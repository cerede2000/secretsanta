package groups

import (
	"fmt"
	"strconv"
	"strings"

	"secretsanta/internal/shared/errs"
	"secretsanta/internal/shared/sanitize"
)

// Messages d'erreur sur le budget.
var (
	msgBudgetNegative = "Le budget ne peut pas être négatif."
	msgBudgetFormat   = "Le budget doit être un montant en euros, par exemple 20 ou 25,50, ou rester vide pour ne fixer aucun budget."
	msgBudgetRange    = fmt.Sprintf("Le budget doit être compris entre %s et %s, ou rester vide pour ne fixer aucun budget.",
		FormatEuros(BudgetMinCents), FormatEuros(BudgetMaxCents))
)

// ParseBudget lit un budget saisi en euros. Une saisie vide (après
// nettoyage) signifie « aucun budget » et renvoie nil. Sont acceptés : un
// entier ou un nombre à une ou deux décimales (virgule ou point), suivi
// éventuellement du symbole €.
func ParseBudget(raw string) (*int64, error) {
	s, err := sanitize.Line(raw, 20, "Le budget")
	if err != nil {
		return nil, err
	}
	s = strings.TrimSpace(strings.TrimSuffix(s, "€"))
	s = strings.ReplaceAll(s, " ", "")
	if s == "" {
		return nil, nil
	}
	if strings.HasPrefix(s, "-") {
		return nil, errs.Validation(msgBudgetNegative)
	}
	whole, frac, hasFrac := strings.Cut(strings.Replace(s, ",", ".", 1), ".")
	if !digitsOnly(whole) || len(whole) > 7 || (hasFrac && (len(frac) < 1 || len(frac) > 2 || !digitsOnly(frac))) {
		return nil, errs.Validation(msgBudgetFormat)
	}
	euros, err := strconv.ParseInt(whole, 10, 64)
	if err != nil {
		return nil, errs.Validation(msgBudgetFormat)
	}
	var cents int64
	if hasFrac {
		if len(frac) == 1 {
			frac += "0"
		}
		cents, _ = strconv.ParseInt(frac, 10, 64)
	}
	total := euros*100 + cents
	if total < BudgetMinCents || total > BudgetMaxCents {
		return nil, errs.Validation(msgBudgetRange)
	}
	return &total, nil
}

func digitsOnly(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}

// FormatEuros écrit un montant en centimes à la française : « 25 € »,
// « 25,50 € », « 10 000 € ».
func FormatEuros(cents int64) string {
	euros, rest := cents/100, cents%100
	digits := strconv.FormatInt(euros, 10)
	var b strings.Builder
	for i, c := range digits {
		if i > 0 && (len(digits)-i)%3 == 0 {
			b.WriteRune(' ') // espace fine insécable
		}
		b.WriteRune(c)
	}
	if rest != 0 {
		fmt.Fprintf(&b, ",%02d", rest)
	}
	b.WriteString(" €")
	return b.String()
}
