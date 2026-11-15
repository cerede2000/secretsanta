// Package code porte le code de vérification de l'inscription par lien (F11.3).
//
// Une inscription en attente reçoit un code de 6 caractères tiré de
// l'alphabet sans caractères ambigus de F2 (token.Alphabet) et un jeton de
// confirmation en un clic de 128 bits. Les deux voyagent dans le même
// courriel (modèle « code de vérification » de F6) et seules leurs empreintes
// à clé (HMAC-SHA-256 avec SIGNING_SECRET, via token.Hash) sont conservées.
//
// Le code est dérivé du jeton de confirmation par une fonction pseudo-aléatoire
// à clé : la page ouverte par le lien peut ainsi afficher le code « déjà
// saisi » sans qu'aucun secret ne soit stocké en clair. Le code reste
// uniforme sur l'alphabet (tirage par rejet sur la sortie HMAC) et ne révèle
// rien du jeton.
//
// Règles : validité 15 minutes, 5 tentatives par code, comparaison à temps
// constant, même refus pour un code faux, expiré, épuisé ou déjà utilisé.
// Renvoi après 60 secondes, 3 codes par heure et par adresse, 5 inscriptions
// par 15 minutes et par adresse IP (limiteur de F2). Le jeton de
// confirmation ne sert qu'une fois et n'agit qu'au POST.
package code

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"strconv"
	"time"

	"secretsanta/internal/access/limiter"
	"secretsanta/internal/access/token"
	"secretsanta/internal/shared/errs"
)

// Règles du code de vérification.
const (
	// Length est la longueur du code (6 caractères).
	Length = token.VerificationCodeLength
	// TTL est la durée de validité d'un code et de son lien de confirmation.
	TTL = 15 * time.Minute
	// MaxAttempts est le nombre de saisies permises pour un même code.
	MaxAttempts = 5
	// ResendDelay est le délai minimal entre deux codes d'une inscription.
	ResendDelay = 60 * time.Second
)

// Plafonds du limiteur de F2.
var (
	// CodesPerAddress : 3 codes par heure et par adresse.
	CodesPerAddress = limiter.Rule{Limit: 3, Window: time.Hour}
	// RegistrationsPerIP : 5 inscriptions par 15 minutes et par adresse IP.
	RegistrationsPerIP = limiter.Rule{Limit: 5, Window: 15 * time.Minute}
	// SubmissionsPerIP : 20 soumissions de formulaire valides par 15 minutes
	// et par adresse IP et par groupe. Ce compteur borne les sondes d'adresse
	// et de nom (« adresse déjà utilisée », « nom déjà inscrit ») ; il est
	// distinct de RegistrationsPerIP, qui ne compte que les créations, pour
	// qu'une personne qui corrige une faute ne consomme pas ses créations.
	SubmissionsPerIP = limiter.Rule{Limit: 20, Window: 15 * time.Minute}
)

// Natures d'empreinte propres à l'inscription : elles séparent ces secrets
// des codes et liens personnels de F2b qui partagent la même clé.
const (
	KindCode    token.Kind = "registration_code"
	KindConfirm token.Kind = "registration_confirm"
)

// MailType est la nature du courriel de vérification dans la file d'envoi.
const MailType = "code_verification"

// ErrRefused est la cause unique de tout refus de vérification : code faux,
// expiré, tentatives épuisées, déjà utilisé, inscription inconnue ou jeton de
// confirmation invalide.
var ErrRefused = errors.New("code: vérification refusée")

// ErrTooSoon signale un renvoi demandé avant ResendDelay.
var ErrTooSoon = errors.New("code: renvoi trop rapproché")

// refusedMessage est le seul message de refus présenté à l'utilisateur.
const refusedMessage = "Ce code n'est pas valide ou n'est plus valable. Vérifie-le ou demande un nouveau code."

// errRefused renvoie le refus uniforme (même nature, même message, même cause).
func errRefused() *errs.Error {
	return errs.Wrap(errs.KindUnauthorized, refusedMessage, ErrRefused)
}

// errTooSoon renvoie le refus d'un renvoi trop rapproché.
func errTooSoon() *errs.Error {
	return errs.Wrap(errs.KindForbidden, "Patiente une minute avant de demander un nouveau code.", ErrTooSoon)
}

// IsRefused rapporte si err est le refus uniforme de vérification.
func IsRefused(err error) bool { return errors.Is(err, ErrRefused) }

func idString(id int64) string { return strconv.FormatInt(id, 10) }

// codeSecret lie le code saisi à son inscription dans l'empreinte.
func codeSecret(registrationID int64, code string) string {
	return idString(registrationID) + ":" + code
}

// hashCode renvoie l'empreinte du code d'une inscription.
func hashCode(key []byte, groupID, registrationID int64, code string) string {
	return token.Hash(key, idString(groupID), KindCode, codeSecret(registrationID, code))
}

// hashConfirm renvoie l'empreinte d'un jeton de confirmation.
func hashConfirm(key []byte, groupID int64, tok string) string {
	return token.Hash(key, idString(groupID), KindConfirm, tok)
}

// deriveDomain sépare la dérivation du code de tout autre usage de la clé.
const deriveDomain = "secretsanta/registration/code/derive/v1"

// deriveCode calcule le code de Length caractères associé à un jeton de
// confirmation : flux HMAC-SHA-256 (clé de l'instance, domaine, groupe,
// inscription, jeton, compteur) et tirage par rejet des octets ≥ 248 pour
// un choix uniforme parmi les 31 symboles.
func deriveCode(key []byte, groupID, registrationID int64, tok string) string {
	alpha := token.Alphabet
	n := len(alpha)
	limit := 256 - 256%n
	out := make([]byte, 0, Length)
	for block := uint64(0); len(out) < Length; block++ {
		m := hmac.New(sha256.New, key)
		for _, part := range []string{deriveDomain, idString(groupID), idString(registrationID), tok} {
			var l [8]byte
			binary.BigEndian.PutUint64(l[:], uint64(len(part)))
			m.Write(l[:])
			m.Write([]byte(part))
		}
		var c [8]byte
		binary.BigEndian.PutUint64(c[:], block)
		m.Write(c[:])
		for _, b := range m.Sum(nil) {
			if int(b) >= limit {
				continue
			}
			out = append(out, alpha[int(b)%n])
			if len(out) == Length {
				break
			}
		}
	}
	return string(out)
}
