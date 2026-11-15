// Package token fournit les briques d'accès participant : jetons de lien
// personnel de 128 bits, codes courts sans caractères ambigus à longueur
// paramétrable, empreintes à clé liées au groupe et comparaison à temps
// constant.
//
// Le secret en clair (jeton ou code) n'existe que dans le lien ou le message
// remis au participant : seule son empreinte HMAC-SHA-256, calculée avec la
// clé de signature de l'instance (SIGNING_SECRET), est conservée. Sans cette
// clé, une fuite de la base ne permet pas de retrouver un code court par
// énumération.
package token

import (
	"crypto/hmac"
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

// ErrMissingKey signale une clé de hachage absente : le service refuse de
// fonctionner sans la clé de signature de l'instance.
var ErrMissingKey = errors.New("token: clé de signature absente")

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

// hashDomain sépare les empreintes de ce paquet de tout autre usage de la clé
// de signature (et de toute autre empreinte HMAC-SHA-256). La version suit la
// construction : v2 est le HMAC à clé, v1 était un SHA-256 sans clé.
const hashDomain = "secretsanta/access/token/v2"

// Hash renvoie l'empreinte (HMAC-SHA-256 avec key, hexadécimal) d'un secret
// lié à un groupe et à une nature. key est la clé de signature de l'instance
// (valeur de SIGNING_SECRET). Le message authentifié est la suite, préfixée
// par leurs longueurs, du domaine, du groupe, de la nature et du secret : le
// même secret donne des empreintes différentes dans deux groupes ou pour deux
// natures, sans ambiguïté de découpage, et une autre clé donne une autre
// empreinte.
func Hash(key []byte, groupID string, kind Kind, secret string) string {
	h := hmac.New(sha256.New, key)
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
