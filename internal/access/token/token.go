// Package token fournit les briques d'accès participant : jetons de lien
// personnel de 128 bits, codes courts sans caractères ambigus à longueur
// paramétrable, empreintes liées au groupe et comparaison à temps constant.
//
// Le secret en clair (jeton ou code) n'existe que dans le lien ou le message
// remis au participant : seule son empreinte est conservée.
package token

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"

	"secretsanta/internal/shared/random"
)

// TokenBits est la taille d'un jeton de lien personnel.
const TokenBits = random.TokenBytes * 8

// Alphabet des codes courts : chiffres et majuscules sans les caractères
// ambigus 0/O et 1/I/L (31 symboles).
const Alphabet = "23456789ABCDEFGHJKMNPQRSTUVWXYZ"

// Longueurs de code utilisées par l'application.
const (
	// LoginCodeLength est la longueur du code court de connexion.
	LoginCodeLength = 8
	// VerificationCodeLength est la longueur du code de vérification
	// d'inscription (F11).
	VerificationCodeLength = 6
)

// Bornes acceptées par GenerateCode.
const (
	MinCodeLength = 4
	MaxCodeLength = 64
)

// Kind distingue la nature d'un secret ; elle entre dans l'empreinte, si bien
// qu'un code ne peut jamais être présenté comme jeton et inversement.
type Kind string

const (
	// KindLink désigne un jeton de lien personnel.
	KindLink Kind = "link"
	// KindCode désigne un code court de connexion.
	KindCode Kind = "code"
)

// ErrInvalidLength signale une longueur de code hors bornes.
var ErrInvalidLength = errors.New("token: longueur de code invalide")

// NewToken renvoie un jeton de lien personnel de 128 bits, encodé en base64
// URL sans remplissage (22 caractères).
func NewToken() (string, error) {
	return random.Token()
}

// GenerateCode renvoie un code de length caractères tirés uniformément dans
// Alphabet avec l'aléa cryptographique.
func GenerateCode(length int) (string, error) {
	if length < MinCodeLength || length > MaxCodeLength {
		return "", fmt.Errorf("%w : %d", ErrInvalidLength, length)
	}
	var b strings.Builder
	b.Grow(length)
	n := int64(len(Alphabet))
	for i := 0; i < length; i++ {
		v, err := random.Int(n)
		if err != nil {
			return "", err
		}
		b.WriteByte(Alphabet[v])
	}
	return b.String(), nil
}

// NormalizeCode met en forme une saisie de code : espaces et tirets retirés,
// lettres mises en majuscules. Elle ne valide rien.
func NormalizeCode(input string) string {
	var b strings.Builder
	b.Grow(len(input))
	for _, r := range input {
		switch r {
		case ' ', '-', '\t':
			continue
		}
		if r >= 'a' && r <= 'z' {
			r -= 'a' - 'A'
		}
		b.WriteRune(r)
	}
	return b.String()
}

// ValidCode rapporte si code (déjà normalisé) a la longueur attendue et ne
// contient que des caractères de Alphabet.
func ValidCode(code string, length int) bool {
	if len(code) != length {
		return false
	}
	for i := 0; i < len(code); i++ {
		if strings.IndexByte(Alphabet, code[i]) < 0 {
			return false
		}
	}
	return true
}

// ValidToken rapporte si tok a la forme d'un jeton de 128 bits.
func ValidToken(tok string) bool {
	if len(tok) != base64.RawURLEncoding.EncodedLen(random.TokenBytes) {
		return false
	}
	b, err := base64.RawURLEncoding.Strict().DecodeString(tok)
	return err == nil && len(b) == random.TokenBytes
}

// hashDomain sépare les empreintes de ce paquet de tout autre usage de SHA-256.
const hashDomain = "secretsanta/access/token/v1"

// Hash renvoie l'empreinte (SHA-256, hexadécimal) d'un secret lié à un groupe
// et à une nature. Le même secret donne des empreintes différentes dans deux
// groupes : une empreinte d'un groupe ne correspond jamais à celle d'un autre.
func Hash(groupID string, kind Kind, secret string) string {
	h := sha256.New()
	for _, part := range []string{hashDomain, groupID, string(kind), secret} {
		var n [8]byte
		binary.BigEndian.PutUint64(n[:], uint64(len(part)))
		h.Write(n[:])
		h.Write([]byte(part))
	}
	return hex.EncodeToString(h.Sum(nil))
}

// Equal compare deux secrets ou empreintes à temps constant : les deux
// valeurs sont d'abord réduites à un condensé de taille fixe, puis comparées
// avec subtle.ConstantTimeCompare, de sorte que la durée ne dépend ni de la
// position de la première différence ni de la longueur des entrées.
func Equal(a, b string) bool {
	da := sha256.Sum256([]byte(a))
	db := sha256.Sum256([]byte(b))
	return subtle.ConstantTimeCompare(da[:], db[:]) == 1
}
