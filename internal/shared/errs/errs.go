// Package errs définit les erreurs typées avec messages français.
package errs

import (
	"errors"
	"log/slog"
	"regexp"
)

// Kind classe une erreur.
type Kind string

const (
	KindValidation   Kind = "validation"
	KindNotFound     Kind = "not_found"
	KindConflict     Kind = "conflict"
	KindForbidden    Kind = "forbidden"
	KindUnauthorized Kind = "unauthorized"
	KindInternal     Kind = "internal"
)

// Error est une erreur typée dont Message est destiné à l'utilisateur (français).
type Error struct {
	Kind    Kind
	Message string
	Err     error

	// Code est un code de message facultatif (clé du catalogue de textes,
	// par exemple "groups.name_required") ; Params fournit ses paramètres.
	// Sans code, les pages utilisent la clé core.errors.<nature>.
	Code   string
	Params map[string]any
}

func (e *Error) Error() string { return e.Message }

// Unwrap expose la cause interne.
func (e *Error) Unwrap() error { return e.Err }

// New crée une erreur typée.
func New(kind Kind, message string) *Error { return &Error{Kind: kind, Message: message} }

// Wrap crée une erreur typée avec une cause.
func Wrap(kind Kind, message string, cause error) *Error {
	return &Error{Kind: kind, Message: message, Err: cause}
}

// WithCode renvoie une copie de l'erreur portant un code de message et des
// paramètres (paires nom, valeur). Le message français et la nature sont
// inchangés.
func (e *Error) WithCode(code string, pairs ...any) *Error {
	c := *e
	c.Code = code
	c.Params = nil
	if len(pairs) > 0 {
		c.Params = map[string]any{}
		for i := 0; i+1 < len(pairs); i += 2 {
			if k, ok := pairs[i].(string); ok {
				c.Params[k] = pairs[i+1]
			}
		}
	}
	return &c
}

// CodeOf renvoie le code de message d'une erreur et ses paramètres ; code vide
// si l'erreur n'en porte pas.
func CodeOf(err error) (string, map[string]any) {
	var e *Error
	if errors.As(err, &e) {
		return e.Code, e.Params
	}
	return "", nil
}

// KindKey renvoie la clé de catalogue de la nature : core.errors.<nature>.
func KindKey(k Kind) string { return "core.errors." + string(k) }

// Kinds liste toutes les natures d'erreur.
var Kinds = []Kind{KindValidation, KindNotFound, KindConflict, KindForbidden, KindUnauthorized, KindInternal}

func Validation(message string) *Error { return New(KindValidation, message) }
func NotFound(message string) *Error   { return New(KindNotFound, message) }
func Conflict(message string) *Error   { return New(KindConflict, message) }
func Forbidden(message string) *Error  { return New(KindForbidden, message) }

// Internal renvoie une erreur interne au message générique. Le message
// générique est tout ce que l'utilisateur voit : la cause est donc journalisée
// ici, dès la création, pour qu'une erreur interne ne reste jamais muette.
func Internal(cause error) *Error {
	if cause != nil {
		slog.Error("erreur interne", "cause", Redact(cause.Error()))
	}
	return Wrap(KindInternal, "Une erreur interne est survenue. Réessayez plus tard.", cause)
}

var (
	redactEmail = regexp.MustCompile(`[^\s@<>"']+@[^\s@<>"']+`)
	redactToken = regexp.MustCompile(`[A-Za-z0-9_-]{20,}`)
	redactURL   = regexp.MustCompile(`https?://\S+`)
)

// Redact rend un message d'erreur publiable dans le journal : adresses
// électroniques, URL (qui peuvent porter un jeton) et longues chaînes opaques
// (jetons, secrets) sont remplacées, et la longueur est bornée.
func Redact(msg string) string {
	msg = redactURL.ReplaceAllString(msg, "<url>")
	msg = redactEmail.ReplaceAllString(msg, "<adresse>")
	msg = redactToken.ReplaceAllString(msg, "<opaque>")
	if r := []rune(msg); len(r) > 300 {
		msg = string(r[:300]) + "…"
	}
	return msg
}

// KindOf renvoie la nature d'une erreur ; KindInternal si elle n'est pas typée.
func KindOf(err error) Kind {
	var e *Error
	if errors.As(err, &e) {
		return e.Kind
	}
	return KindInternal
}

// Is rapporte si err est de la nature donnée.
func Is(err error, kind Kind) bool {
	var e *Error
	return errors.As(err, &e) && e.Kind == kind
}

// UserMessage renvoie un message affichable en français.
func UserMessage(err error) string {
	var e *Error
	if errors.As(err, &e) {
		return e.Message
	}
	return Internal(err).Message
}
