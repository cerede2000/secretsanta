package adjust

import (
	"fmt"
	"strings"
)

// Textes français du module. Ils sont des constantes Go ; l'orchestrateur
// peut les reprendre dans web/static/i18n/adjust/{fr,en}.json à partir des
// codes (Code*) et des comptes de l'aperçu.

// Codes de message (pour les catalogues de textes).
const (
	CodeImpossible = "adjust.impossible"
	CodeCapacity   = "adjust.capacity"
)

const (
	msgAuth          = "Authentification requise."
	msgForbidden     = "Tu n'as pas le droit d'effectuer cette action."
	msgNotDone       = "Le tirage n'est pas encore fait : il n'y a rien à ajuster."
	msgConfirm       = "L'ajustement doit être confirmé après lecture de l'aperçu."
	msgChanged       = "Le tirage a changé entre-temps. Recharge l'aperçu et recommence."
	msgUnknownKind   = "Choisis de retirer ou d'ajouter une personne."
	msgNotInGroup    = "Cette personne n'existe pas dans le groupe."
	msgNotInDraw     = "Cette personne ne fait pas partie du tirage actuel."
	msgAlreadyInDraw = "Cette personne fait déjà partie du tirage actuel."
	msgNoRemover     = "Le retrait d'une personne n'est pas disponible pour le moment."
	msgFullDraw      = "Tu peux refaire un tirage complet après avoir assoupli les règles : l'ancien tirage sera archivé et toutes les cibles changeront."
	msgNoFix         = "Aucun ajustement du tirage n'est possible sans enfreindre une règle."
)

const (
	mailSubject      = "Le tirage a été ajusté"
	mailNewTarget    = "L'organisateur a ajusté le tirage de ton groupe : tu as une nouvelle cible à découvrir. Connecte-toi à ton espace personnel pour la découvrir."
	mailFewerTargets = "L'organisateur a ajusté le tirage de ton groupe : tu as désormais une cible en moins. Connecte-toi à ton espace personnel pour voir ta cible."
	mailDiscretion   = "Par discrétion, ce message ne nomme personne d'autre que toi."
)

func mailGreeting(first string) string {
	if strings.TrimSpace(first) == "" {
		return "Bonjour,"
	}
	return "Bonjour " + first + ","
}

// summary écrit l'aperçu en français, sans aucun nom : « 1 personne verra
// sa cible changer, elle l'avait déjà découverte ».
func summary(affected, revealed int) string {
	switch {
	case affected == 0:
		return "Aucune personne ne verra sa cible changer."
	case affected == 1 && revealed == 1:
		return "1 personne verra sa cible changer, elle l'avait déjà découverte."
	case affected == 1:
		return "1 personne verra sa cible changer, elle ne l'avait pas encore découverte."
	case revealed == 0:
		return fmt.Sprintf("%d personnes verront leur cible changer, aucune ne l'avait encore découverte.", affected)
	case revealed == affected:
		return fmt.Sprintf("%d personnes verront leur cible changer, toutes l'avaient déjà découverte.", affected)
	case revealed == 1:
		return fmt.Sprintf("%d personnes verront leur cible changer, dont 1 qui l'avait déjà découverte.", affected)
	default:
		return fmt.Sprintf("%d personnes verront leur cible changer, dont %d qui l'avaient déjà découverte.", affected, revealed)
	}
}
