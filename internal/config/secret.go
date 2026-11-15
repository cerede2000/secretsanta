package config

import (
	"fmt"
	"log/slog"
)

// Secret enveloppe une valeur confidentielle. Toutes ses représentations
// texte (fmt, JSON, journaux slog) sont masquées ; seule Reveal donne la valeur.
type Secret struct{ v string }

const redacted = "***"

// NewSecret construit un Secret.
func NewSecret(v string) Secret { return Secret{v: v} }

// Reveal renvoie la valeur en clair. À n'utiliser qu'au point d'emploi.
func (s Secret) Reveal() string { return s.v }

// IsSet indique si une valeur est définie.
func (s Secret) IsSet() bool { return s.v != "" }

func (s Secret) mask() string {
	if s.v == "" {
		return "(non défini)"
	}
	return redacted
}

// String masque la valeur.
func (s Secret) String() string { return s.mask() }

// GoString masque la valeur pour %#v.
func (s Secret) GoString() string { return s.mask() }

// Format masque la valeur pour tous les verbes fmt.
func (s Secret) Format(f fmt.State, _ rune) { _, _ = f.Write([]byte(s.mask())) }

// MarshalText masque la valeur (JSON, texte).
func (s Secret) MarshalText() ([]byte, error) { return []byte(s.mask()), nil }

// MarshalJSON masque la valeur.
func (s Secret) MarshalJSON() ([]byte, error) { return []byte(`"` + s.mask() + `"`), nil }

// LogValue masque la valeur pour slog.
func (s Secret) LogValue() slog.Value { return slog.StringValue(s.mask()) }
