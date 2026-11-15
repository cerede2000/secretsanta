// Package dashboard porte le tableau de bord de l'administrateur de groupe
// (F10.1) : l'état de la fête sans jamais montrer une paire tireur-tiré, le
// contrôle du tirage sans nom, la marque « lien remis » des personnes sans
// courriel et les pages qui les présentent.
//
// Règles tenues partout ici : l'administrateur ne reçoit jamais qui offre à
// qui (aucun type de sortie ne porte une paire, un tiré ou un tireur par
// cible) ; toute méthode exige l'administrateur du groupe concerné et refuse
// tout autre groupe ; toute action sensible est consignée dans le journal.
package dashboard

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	"secretsanta/internal/admin/audit"
	"secretsanta/internal/draw"
	"secretsanta/internal/participants"
	"secretsanta/internal/reveal"
	"secretsanta/internal/rules"
	"secretsanta/internal/shared/clock"
	"secretsanta/internal/shared/errs"
	"secretsanta/internal/shared/ports"
)

// Participants est la part du service des participants (F3) utilisée ici.
type Participants interface {
	List(ctx context.Context, a participants.Actor, groupID int64) ([]participants.Participant, error)
	Roster(ctx context.Context, groupID int64) (*participants.Roster, error)
}

// Draws est la part du service de tirage (F4) utilisée ici : des faits, jamais
// de paire.
type Draws interface {
	AdminFacts(ctx context.Context, a participants.Actor, groupID int64) (*draw.Facts, error)
}

// Reveal est la part du service de révélation (F4d) utilisée ici.
type Reveal interface {
	Facts(ctx context.Context, a participants.Actor, groupID int64) (*reveal.Facts, error)
}

// Constraints est la part du service de règles (F3) utilisée ici.
type Constraints interface {
	BuildConstraints(ctx context.Context, groupID int64) (rules.Constraints, error)
}

// Options rassemble les dépendances.
type Options struct {
	DB           *sql.DB
	Clock        clock.Clock
	Participants Participants
	Draws        Draws
	Constraints  Constraints
	Audit        *audit.Service
	// Facultatifs : sans eux, la révélation est vue comme non faite et le
	// compteur d'inscriptions en attente reste à zéro.
	Reveal        Reveal
	Registrations ports.RegistrationState
}

// Service construit le tableau de bord et le contrôle du tirage.
type Service struct{ o Options }

// New vérifie les dépendances.
func New(o Options) (*Service, error) {
	if o.DB == nil || o.Clock == nil || o.Participants == nil || o.Draws == nil || o.Constraints == nil || o.Audit == nil {
		return nil, errors.New("dashboard: dépendance manquante")
	}
	return &Service{o: o}, nil
}

// Messages français des erreurs du service.
const (
	msgForbidden = "Vous n'avez pas le droit d'effectuer cette action."
	msgNotDone   = "Le tirage n'est pas encore fait."
	msgNoPerson  = "Cette personne n'existe pas dans ce groupe."
	msgNoEmail   = "Seule une personne sans courriel a un lien à remettre."
)

func requireAdmin(a participants.Actor, groupID int64) error {
	if a.Kind != participants.ActorAdmin || a.GroupID != groupID || groupID == 0 {
		return errs.Forbidden(msgForbidden)
	}
	return nil
}

// MailState est l'état du dernier courriel adressé à une personne.
type MailState string

// États : none (aucun courriel), pending, sent, failed.
const (
	MailNone    MailState = "none"
	MailPending MailState = "pending"
	MailSent    MailState = "sent"
	MailFailed  MailState = "failed"
)

// Person est une ligne du tableau de bord. Aucune paire : « cible vue » dit
// seulement que la personne a découvert toutes ses cibles, jamais lesquelles.
type Person struct {
	ID        int64
	FirstName string
	Name      string
	HasEmail  bool
	Mail      MailState
	// LinkOpened : la personne s'est connectée avec son lien ou son code.
	LinkOpened bool
	// HandedOver : le lien d'une personne sans courriel a été remis.
	HandedOver bool
	// TargetsSeen : la personne a découvert toutes ses cibles.
	TargetsSeen   bool
	LetterWritten bool
	// LetterRead : la lettre de la personne a été lue par son tireur.
	LetterRead bool
}

// View est le tableau de bord : des comptes et des états par personne.
type View struct {
	GroupID int64
	// Registered est le nombre de personnes inscrites.
	Registered int

	DrawDone bool
	DrawYear int
	DrawnAt  time.Time

	LinksOpened int
	// TargetsSeen et Targets : cibles découvertes et cibles du tirage.
	TargetsSeen int
	Targets     int

	RevealDone bool
	RevealAt   *time.Time

	LettersWritten int
	LettersRead    int

	MailsSent    int
	MailsFailed  int
	MailsPending int

	// NoEmail : personnes sans courriel ; NoEmailHandedOver : celles dont le
	// lien a été remis.
	NoEmail           int
	NoEmailHandedOver int

	// PendingRegistrations : inscriptions par lien en attente (F11).
	PendingRegistrations int

	People []Person
}

// Dashboard construit le tableau de bord du groupe.
func (s *Service) Dashboard(ctx context.Context, a participants.Actor, groupID int64) (*View, error) {
	if err := requireAdmin(a, groupID); err != nil {
		return nil, err
	}
	list, err := s.o.Participants.List(ctx, a, groupID)
	if err != nil {
		return nil, err
	}
	facts, err := s.o.Draws.AdminFacts(ctx, a, groupID)
	if err != nil {
		return nil, err
	}
	act, err := loadActivity(ctx, s.o.DB, groupID)
	if err != nil {
		return nil, err
	}
	v := &View{GroupID: groupID, Registered: len(list)}
	v.DrawDone, v.DrawYear, v.DrawnAt = facts.Done, facts.Year, facts.DrawnAt
	v.Targets, v.TargetsSeen = facts.Targets, facts.Discovered
	if s.o.Reveal != nil {
		rf, err := s.o.Reveal.Facts(ctx, a, groupID)
		if err != nil {
			return nil, err
		}
		v.RevealDone, v.RevealAt = rf.Open, rf.RevealAt
	}
	if s.o.Registrations != nil {
		n, err := s.o.Registrations.PendingCount(ctx, groupID)
		if err != nil {
			return nil, errs.Internal(err)
		}
		v.PendingRegistrations = n
	}
	for _, p := range list {
		row := Person{
			ID: p.ID, FirstName: p.FirstName, Name: p.Name,
			HasEmail:      strings.TrimSpace(p.Email) != "",
			Mail:          MailNone,
			LinkOpened:    act.linkOpened[p.ID],
			HandedOver:    act.handedOver[p.ID],
			TargetsSeen:   act.targetsSeen[p.ID],
			LetterWritten: act.letterWritten[p.ID],
			LetterRead:    act.letterRead[p.ID],
		}
		if m, ok := act.mail[p.ID]; ok {
			row.Mail = m.last
		}
		switch row.Mail {
		case MailSent:
			v.MailsSent++
		case MailFailed:
			v.MailsFailed++
		case MailPending:
			v.MailsPending++
		}
		if row.LinkOpened {
			v.LinksOpened++
		}
		if row.LetterWritten {
			v.LettersWritten++
		}
		if row.LetterRead {
			v.LettersRead++
		}
		if !row.HasEmail {
			v.NoEmail++
			if row.HandedOver {
				v.NoEmailHandedOver++
			}
		}
		v.People = append(v.People, row)
	}
	return v, nil
}

// MarkLinkHandedOver consigne que le lien d'une personne sans courriel lui a
// été remis en main propre. La marque et sa trace au journal partagent une
// transaction ; la personne doit appartenir au groupe de l'administrateur.
func (s *Service) MarkLinkHandedOver(ctx context.Context, a participants.Actor, groupID, participantID int64) error {
	if err := requireAdmin(a, groupID); err != nil {
		return err
	}
	list, err := s.o.Participants.List(ctx, a, groupID)
	if err != nil {
		return err
	}
	var found *participants.Participant
	for i := range list {
		if list[i].ID == participantID {
			found = &list[i]
		}
	}
	if found == nil {
		return errs.NotFound(msgNoPerson)
	}
	if strings.TrimSpace(found.Email) != "" {
		return errs.Validation(msgNoEmail)
	}
	tx, err := s.o.DB.BeginTx(ctx, nil)
	if err != nil {
		return errs.Internal(err)
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, `INSERT INTO admin_link_handover (group_id, participant_id, handed_at) VALUES (?, ?, ?)
		ON CONFLICT (group_id, participant_id) DO NOTHING`, groupID, participantID, s.o.Clock.Now().UTC().UnixNano()); err != nil {
		return errs.Internal(err)
	}
	if err := s.o.Audit.RecordTx(ctx, tx, groupID, audit.ActionLinkHandedOver, participantID); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return errs.Internal(err)
	}
	return nil
}
