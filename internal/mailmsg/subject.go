package mailmsg

import (
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/text/unicode/norm"
)

// MaxSubjectLength est la longueur maximale d'un objet, en caractères.
const MaxSubjectLength = 60

// normalizeSubject refuse toute injection puis normalise l'objet (NFC, bords
// retirés).
func normalizeSubject(s string) (string, error) {
	if err := checkField(s, "L'objet"); err != nil {
		return "", err
	}
	return strings.TrimSpace(norm.NFC.String(s)), nil
}

// LintSubject vérifie la sobriété d'un objet : non vide, au plus 60
// caractères, aucun mot de deux lettres ou plus entièrement en majuscules,
// aucun « !!! », aucun retour à la ligne ni caractère de contrôle.
func LintSubject(subject string) error {
	s, err := normalizeSubject(subject)
	if err != nil {
		return err
	}
	if s == "" {
		return fail(ErrSubject, "L'objet est obligatoire.")
	}
	if n := utf8.RuneCountInString(s); n > MaxSubjectLength {
		return fail(ErrSubject, fmt.Sprintf("L'objet ne doit pas dépasser %d caractères (%d).", MaxSubjectLength, n))
	}
	if strings.Contains(s, "!!!") {
		return fail(ErrSubject, "L'objet ne doit pas contenir « !!! ».")
	}
	if w := shoutedWord(s); w != "" {
		return fail(ErrSubject, fmt.Sprintf("L'objet ne doit pas contenir de mot en majuscules (« %s »).", w))
	}
	return nil
}

// shoutedWord renvoie le premier mot d'au moins deux lettres dont toutes les
// lettres sont majuscules, ou "".
func shoutedWord(s string) string {
	words := strings.FieldsFunc(s, func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) })
	for _, w := range words {
		letters, upper := 0, 0
		for _, r := range w {
			if unicode.IsLetter(r) {
				letters++
				if unicode.IsUpper(r) {
					upper++
				}
			}
		}
		if letters >= 2 && upper == letters {
			return w
		}
	}
	return ""
}
