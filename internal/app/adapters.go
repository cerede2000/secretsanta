package app

import (
	"context"
	"errors"

	"secretsanta/internal/participants"
	"secretsanta/internal/shared/errs"
)

// drawNotDone est un adaptateur TEMPORAIRE du port ports.DrawState : il
// répond toujours « tirage non fait ». Il sera remplacé par l'implémentation
// réelle de la vague du tirage (F4) ; tant qu'il est en place, les règles
// « modification après tirage » de participants ne se déclenchent jamais.
type drawNotDone struct{}

// IsDrawDone renvoie toujours faux.
func (drawNotDone) IsDrawDone(context.Context, int64) (bool, error) { return false, nil }

// participantDirectory adapte participants.Service au port rules.Directory
// (lecture du rôle de tirage). Aucun lecteur dédié n'existe dans participants :
// l'adaptateur passe par Get avec l'acteur administrateur du groupe, ce qui
// reste borné au groupe demandé.
type participantDirectory struct{ svc *participants.Service }

// DrawRole renvoie le rôle ; ok est faux si le participant n'existe pas dans
// le groupe.
func (d participantDirectory) DrawRole(ctx context.Context, groupID, participantID int64) (string, bool, error) {
	p, err := d.svc.Get(ctx, participants.AdminActor(groupID), groupID, participantID)
	if err != nil {
		var e *errs.Error
		if errors.As(err, &e) && e.Kind == errs.KindNotFound {
			return "", false, nil
		}
		return "", false, err
	}
	return string(p.Role), true, nil
}
