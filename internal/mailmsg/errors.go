// Package mailmsg fabrique des courriels conformes aux attentes des filtres
// antispam : multipart/alternative (texte puis HTML) en UTF-8 quoted-printable,
// en-têtes complets, objet sobre encodé RFC 2047, liens et HTML contrôlés,
// parité entre le texte et le HTML, refus de toute injection d'en-tête.
//
// La date, le Message-ID et la frontière MIME proviennent de l'horloge et de
// l'aléa injectés : avec les mêmes entrées, le message produit est identique.
//
// Le paquet ne décide ni du contenu des modèles ni de l'envoi (F6) ; il ne
// signe pas (internal/maildkim).
package mailmsg

import (
	"errors"

	"secretsanta/internal/shared/errs"
)

// Catégories d'erreurs, utilisables avec errors.Is. Chaque refus est un
// *errs.Error de nature KindValidation (message français) qui enveloppe l'une
// d'elles.
var (
	// ErrHeaderInjection : un retour à la ligne ou un caractère de contrôle
	// dans un nom, un objet ou une adresse.
	ErrHeaderInjection = errors.New("mailmsg: tentative d'injection d'en-tête")
	// ErrAddress : adresse courriel invalide ou expéditeur non aligné.
	ErrAddress = errors.New("mailmsg: adresse invalide")
	// ErrSubject : objet refusé par le contrôle de sobriété.
	ErrSubject = errors.New("mailmsg: objet refusé")
	// ErrHTML : HTML refusé (ressource distante, script, pixel, alt manquant,
	// structure invalide).
	ErrHTML = errors.New("mailmsg: HTML refusé")
	// ErrLink : lien refusé (non https, hors de l'hôte public, raccourcisseur).
	ErrLink = errors.New("mailmsg: lien refusé")
	// ErrParity : le texte et le HTML ne portent pas les mêmes informations.
	ErrParity = errors.New("mailmsg: texte et HTML non équivalents")
	// ErrBody : corps vide ou UTF-8 invalide.
	ErrBody = errors.New("mailmsg: corps invalide")
	// ErrConfig : configuration du constructeur invalide.
	ErrConfig = errors.New("mailmsg: configuration invalide")
)

func fail(cat error, message string) error {
	return errs.Wrap(errs.KindValidation, message, cat)
}
