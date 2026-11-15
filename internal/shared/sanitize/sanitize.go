// Package sanitize nettoie et valide les saisies.
package sanitize

import (
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/text/unicode/norm"

	"secretsanta/internal/shared/errs"
)

// clean normalise en NFC, supprime les caractères de contrôle et de format,
// ramène toute suite d'espaces à un seul espace et retire les bords.
// Si multiline, les sauts de ligne sont conservés (séquences de blancs par
// ligne normalisées, plus de deux sauts consécutifs réduits à deux).
func clean(s string, multiline bool) string {
	s = norm.NFC.String(strings.ToValidUTF8(s, ""))
	s = strings.ReplaceAll(strings.ReplaceAll(s, "\r\n", "\n"), "\r", "\n")
	var b strings.Builder
	space := false
	nl := 0
	for _, r := range s {
		switch {
		case r == '\n' && multiline:
			nl++
			space = false
		case unicode.IsSpace(r):
			space = true
		case unicode.IsControl(r), unicode.Is(unicode.Cf, r):
			// supprimé
		default:
			if nl > 0 {
				if b.Len() > 0 {
					b.WriteString(strings.Repeat("\n", min(nl, 2)))
				}
				nl, space = 0, false
			} else if space && b.Len() > 0 {
				b.WriteByte(' ')
			}
			space = false
			b.WriteRune(r)
		}
	}
	return b.String()
}

func check(s string, max int, field string) (string, error) {
	if utf8.RuneCountInString(s) > max {
		return "", errs.Validation(fmt.Sprintf("%s ne doit pas dépasser %d caractères.", field, max))
	}
	return s, nil
}

// Line nettoie une saisie d'une ligne et refuse une longueur (en caractères)
// supérieure à max.
func Line(s string, max int, field string) (string, error) {
	return check(clean(s, false), max, field)
}

// Text nettoie une saisie multiligne et refuse une longueur supérieure à max.
func Text(s string, max int, field string) (string, error) {
	return check(clean(s, true), max, field)
}

// Required comme Line mais refuse une valeur vide.
func Required(s string, max int, field string) (string, error) {
	v, err := Line(s, max, field)
	if err != nil {
		return "", err
	}
	if v == "" {
		return "", errs.Validation(fmt.Sprintf("%s est obligatoire.", field))
	}
	return v, nil
}
