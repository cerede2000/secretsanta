package reminders

import (
	"context"
	"sort"

	"secretsanta/internal/participants"
	"secretsanta/internal/shared/errs"
)

// ManualItem est le sort d'une personne dans une relance manuelle.
type ManualItem struct {
	ParticipantID int64
	Name          string
	Outcome       Outcome
}

// ManualResult est le résultat d'une relance manuelle : une ligne par personne
// demandée, dans l'ordre de la demande, sans doublon.
type ManualResult struct{ Items []ManualItem }

// Count compte les personnes ayant cette issue.
func (r ManualResult) Count(o Outcome) int {
	n := 0
	for _, it := range r.Items {
		if it.Outcome == o {
			n++
		}
	}
	return n
}

// IDs liste les personnes ayant cette issue.
func (r ManualResult) IDs(o Outcome) []int64 {
	var out []int64
	for _, it := range r.Items {
		if it.Outcome == o {
			out = append(out, it.ParticipantID)
		}
	}
	return out
}

// Remind relance à la main une personne ou une liste (l'appelant, F10, a
// vérifié la session d'administration). Les personnes déjà relancées
// aujourd'hui, désinscrites des relances, à adresse refusée, sans courriel ou
// déjà à jour sont signalées dans le résultat, sans courriel.
func (e *Engine) Remind(ctx context.Context, groupID int64, kind Kind, ids []int64) (ManualResult, error) {
	var res ManualResult
	switch kind {
	case KindLink, KindLetter, KindRead:
	default:
		return res, errs.Validation("Ce type de relance n'existe pas.")
	}
	g, err := e.o.Directory.Group(ctx, groupID)
	if err != nil {
		return res, err
	}
	people, err := e.o.Directory.Participants(ctx, groupID)
	if err != nil {
		return res, err
	}
	byID := map[int64]participants.Participant{}
	for _, p := range people {
		byID[p.ID] = p
	}
	var unread map[int64]bool
	if kind == KindRead {
		if unread, err = e.o.Facts.UnreadGivers(ctx, groupID); err != nil {
			return res, err
		}
	}
	seen := map[int64]bool{}
	for _, id := range ids {
		if seen[id] {
			continue
		}
		seen[id] = true
		p, ok := byID[id]
		if !ok {
			res.Items = append(res.Items, ManualItem{ParticipantID: id, Outcome: OutcomeUnknown})
			continue
		}
		it := ManualItem{ParticipantID: id, Name: p.Name}
		late, err := e.late(ctx, groupID, p, kind, unread)
		if err != nil {
			return res, errs.Internal(err)
		}
		if !late {
			it.Outcome = OutcomeNotNeeded
		} else if it.Outcome, err = e.send(ctx, g, p, kind, false); err != nil {
			return res, err
		}
		res.Items = append(res.Items, it)
	}
	return res, nil
}

// Person est une personne d'une liste destinée à l'administrateur.
type Person struct {
	ID        int64
	Name      string
	FirstName string
}

// RevealLaggards liste les tireurs qui n'ont pas ouvert toutes leurs cibles.
// Les personnes en réception seule n'y figurent jamais.
func (e *Engine) RevealLaggards(ctx context.Context, groupID int64) ([]Person, error) {
	unread, err := e.o.Facts.UnreadGivers(ctx, groupID)
	if err != nil {
		return nil, err
	}
	people, err := e.o.Directory.Participants(ctx, groupID)
	if err != nil {
		return nil, err
	}
	var out []Person
	for _, p := range people {
		if unread[p.ID] && p.Role != participants.RoleReceptionSeule {
			out = append(out, Person{ID: p.ID, Name: p.Name, FirstName: p.FirstName})
		}
	}
	return out, nil
}

// HandDelivery est une personne sans courriel encore en retard : l'administrateur
// la prévient lui-même (donnée exposée à F10).
type HandDelivery struct {
	Person
	LinkNotOpened    bool
	LetterNotWritten bool
}

// ManualDeliveryList liste les personnes sans courriel qui n'ont pas ouvert
// leur lien ou pas écrit leur lettre, triées par nom.
func (e *Engine) ManualDeliveryList(ctx context.Context, groupID int64) ([]HandDelivery, error) {
	people, err := e.o.Directory.Participants(ctx, groupID)
	if err != nil {
		return nil, err
	}
	var out []HandDelivery
	for _, p := range people {
		if p.Email != "" {
			continue
		}
		l, err := e.late(ctx, groupID, p, KindLink, nil)
		if err != nil {
			return nil, errs.Internal(err)
		}
		w, err := e.late(ctx, groupID, p, KindLetter, nil)
		if err != nil {
			return nil, errs.Internal(err)
		}
		if l || w {
			out = append(out, HandDelivery{Person: Person{ID: p.ID, Name: p.Name, FirstName: p.FirstName}, LinkNotOpened: l, LetterNotWritten: w})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}
