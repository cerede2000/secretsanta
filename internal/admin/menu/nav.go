// Package menu déclare les entrées de navigation d'administration de F10
// (Tableau, Tirage, Plus) et sert la feuille « Plus » (screen-admin-menu).
//
// Le paquet ne connaît ni les routes ni l'analyse des gabarits : l'orchestrateur
// fournit un Renderer et monte les gestionnaires.
package menu

import (
	"strings"

	"secretsanta/internal/shared/adminnav"
)

// SlugPlaceholder remplace le slug du groupe dans les destinations déclarées
// (le registre est commun à tous les groupes) ; ResolveNav le substitue.
const SlugPlaceholder = "{slug}"

// Destinations des entrées de la barre de navigation.
const (
	DashboardPath = "/{slug}/admin"
	DrawPath      = "/{slug}/admin/tirage"
	MorePath      = "/{slug}/admin/plus"
)

// DeclareNav déclare Tableau, Tirage et Plus dans le registre de F1b.
// « Participants » est déclarée à part (DeclareParticipants) et « Inscriptions »
// par F11c.
func DeclareNav(r *adminnav.Registry) {
	r.Declare(adminnav.Entry{Key: "tableau", Label: "layout.nav.dashboard", Icon: "dashboard", Rank: adminnav.RankDashboard, Destination: DashboardPath})
	r.Declare(adminnav.Entry{Key: "tirage", Label: "layout.nav.draw", Icon: "draw", Rank: adminnav.RankDraw, Destination: DrawPath})
	r.Declare(adminnav.Entry{Key: "plus", Label: "layout.nav.more", Icon: "more", Rank: adminnav.RankMore, Destination: MorePath})
}

// ResolveNav remplace {slug} dans les destinations des entrées.
func ResolveNav(items []adminnav.Item, slug string) []adminnav.Item {
	out := make([]adminnav.Item, len(items))
	for i, it := range items {
		it.Destination = strings.ReplaceAll(it.Destination, SlugPlaceholder, slug)
		out[i] = it
	}
	return out
}
