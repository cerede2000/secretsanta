package menu

import "secretsanta/internal/shared/adminnav"

// ParticipantsPath est la destination de l'entrée « Participants ».
const ParticipantsPath = "/{slug}/admin/participants"

// DeclareParticipants déclare l'entrée « Participants » du registre de
// navigation d'administration. Elle ne dépend d'aucune autre entrée ni du
// tableau de bord.
func DeclareParticipants(r *adminnav.Registry) {
	r.Declare(adminnav.Entry{Key: "participants", Label: "layout.nav.participants", Icon: "participants", Rank: adminnav.RankParticipants, Destination: ParticipantsPath})
}
