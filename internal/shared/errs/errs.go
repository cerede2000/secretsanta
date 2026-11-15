// Package errs définit les erreurs typées avec messages français.
package errs

import "errors"

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

func Validation(message string) *Error { return New(KindValidation, message) }
func NotFound(message string) *Error   { return New(KindNotFound, message) }
func Conflict(message string) *Error   { return New(KindConflict, message) }
func Forbidden(message string) *Error  { return New(KindForbidden, message) }

// Internal renvoie une erreur interne au message générique.
func Internal(cause error) *Error {
	return Wrap(KindInternal, "Une erreur interne est survenue. Réessayez plus tard.", cause)
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
