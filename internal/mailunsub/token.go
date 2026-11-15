// Package mailunsub porte la désinscription par type de mail (F6.4) : liens
// signés HMAC sans connexion, service IsUnsubscribed, page de confirmation
// (screen-desinscription) et règle des mails essentiels, jamais désactivables.
package mailunsub

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"strconv"
	"strings"
)

// Types de désinscription (non essentiels).
const (
	TypeRelances     = "relances"
	TypeNouvelleIdee = "nouvelle_idee"
)

// ErrUnknownType : seuls relances et nouvelle_idee se coupent.
var ErrUnknownType = errors.New("mailunsub: type de mail inconnu ou essentiel")

// ErrBadToken : jeton mal formé ou signature fausse (aucun détail).
var ErrBadToken = errors.New("mailunsub: lien invalide")

// ValidType dit si t est un type désactivable.
func ValidType(t string) bool { return t == TypeRelances || t == TypeNouvelleIdee }

// domain sépare le domaine de cette signature des autres usages du secret.
const domain = "secretsanta/mailunsub/v1"

// Claims est ce que couvre la signature : groupe, participant et type.
type Claims struct {
	GroupID       int64
	ParticipantID int64
	Type          string
}

// Signer signe et vérifie les jetons de désinscription.
type Signer struct{ key []byte }

// NewSigner dérive une clé propre au module du secret de signature de F1.
func NewSigner(signingKey []byte) *Signer {
	m := hmac.New(sha256.New, signingKey)
	m.Write([]byte(domain))
	return &Signer{key: m.Sum(nil)}
}

func (s *Signer) mac(payload string) []byte {
	m := hmac.New(sha256.New, s.key)
	m.Write([]byte(payload))
	return m.Sum(nil)
}

var enc = base64.RawURLEncoding

// Sign produit le jeton « charge.signature » (base64 URL, sans « / »).
func (s *Signer) Sign(c Claims) (string, error) {
	if !ValidType(c.Type) || c.GroupID <= 0 || c.ParticipantID <= 0 {
		return "", ErrUnknownType
	}
	payload := "v1|" + strconv.FormatInt(c.GroupID, 10) + "|" + strconv.FormatInt(c.ParticipantID, 10) + "|" + c.Type
	return enc.EncodeToString([]byte(payload)) + "." + enc.EncodeToString(s.mac(payload)), nil
}

// Verify contrôle la signature en temps constant puis lit la charge.
func (s *Signer) Verify(tok string) (Claims, error) {
	a, b, ok := strings.Cut(tok, ".")
	if !ok || a == "" || b == "" || len(tok) > 512 {
		return Claims{}, ErrBadToken
	}
	raw, err := enc.DecodeString(a)
	if err != nil {
		return Claims{}, ErrBadToken
	}
	sig, err := enc.DecodeString(b)
	if err != nil || !hmac.Equal(sig, s.mac(string(raw))) {
		return Claims{}, ErrBadToken
	}
	parts := strings.Split(string(raw), "|")
	if len(parts) != 4 || parts[0] != "v1" || !ValidType(parts[3]) {
		return Claims{}, ErrBadToken
	}
	g, e1 := strconv.ParseInt(parts[1], 10, 64)
	p, e2 := strconv.ParseInt(parts[2], 10, 64)
	if e1 != nil || e2 != nil || g <= 0 || p <= 0 {
		return Claims{}, ErrBadToken
	}
	return Claims{GroupID: g, ParticipantID: p, Type: parts[3]}, nil
}
