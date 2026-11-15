// Package admin porte la page d'administration des inscriptions par lien
// (F11c.2, screen-admin-inscriptions) : interrupteur, lien et QR code,
// renouvellement, limites facultatives et liste des inscriptions en attente.
//
// Le paquet ne connaît ni les routes ni l'analyse des gabarits : l'orchestrateur
// fournit un Renderer et appelle Register.
package admin

import (
	"strings"

	"secretsanta/internal/shared/adminnav"
)

// Destination de l'entrée « Inscriptions » ({slug} est remplacé par
// ResolveNav) et clé de l'entrée dans le registre.
const (
	NavKey           = "inscriptions"
	RegistrationPath = "/{slug}/admin/inscriptions"
	slugPlaceholder  = "{slug}"
)

// DeclareNav déclare l'entrée « Inscriptions » du registre de navigation
// d'administration, seule : elle ne dépend d'aucune autre entrée ni de F10.
func DeclareNav(r *adminnav.Registry) {
	r.Declare(adminnav.Entry{Key: NavKey, Label: "layout.nav.registrations", Icon: "registrations",
		Rank: adminnav.RankRegistrations, Destination: RegistrationPath})
}

// ResolveNav remplace {slug} dans les destinations des entrées.
func ResolveNav(items []adminnav.Item, slug string) []adminnav.Item {
	out := make([]adminnav.Item, len(items))
	for i, it := range items {
		it.Destination = strings.ReplaceAll(it.Destination, slugPlaceholder, slug)
		out[i] = it
	}
	return out
}
