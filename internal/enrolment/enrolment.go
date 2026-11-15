// Package enrolment porte la création vérifiée d'un participant (F3d.1) avec
// son lien et son code personnels, et le service générique de QR code.
package enrolment

import (
	"context"
	"database/sql"
	"errors"
	"strconv"
	"time"

	"secretsanta/internal/participants"
	"secretsanta/internal/shared/errs"
)

// Participants est la part du service des participants utilisée ici.
type Participants interface {
	Register(ctx context.Context, groupID int64, in participants.Input, verifiedAt time.Time) (*participants.Participant, error)
}

// Credentials est la part du service d'accès utilisée ici.
type Credentials interface {
	IssueLink(ctx context.Context, groupID, participantID string) (string, error)
	IssueCode(ctx context.Context, groupID, participantID string) (string, error)
}

// LinkFunc construit le lien personnel absolu à partir du groupe et du jeton.
type LinkFunc func(slug, token string) string

// Service crée des participants vérifiés.
type Service struct {
	db    *sql.DB
	parts Participants
	creds Credentials
	link  LinkFunc
}

// NewService construit le service.
func NewService(d *sql.DB, p Participants, c Credentials, link LinkFunc) *Service {
	return &Service{db: d, parts: p, creds: c, link: link}
}

// Created est le résultat d'une création vérifiée. Le jeton et le code sont en
// clair et ne sont pas conservés ailleurs.
type Created struct {
	Participant *participants.Participant
	Link        string
	Code        string
}

const msgRole = "Ce rôle n'est pas permis à l'inscription."

// CreateVerified crée le participant (joined_at = verifiedAt) avec les mêmes
// règles que l'ajout manuel, puis son lien et son code personnels. Le rôle
// facultatif ne peut être que normal, reception_seule ou offre_plus.
func (s *Service) CreateVerified(ctx context.Context, groupID int64, in participants.Input, verifiedAt time.Time) (*Created, error) {
	switch in.Role {
	case "", participants.RoleNormal, participants.RoleReceptionSeule, participants.RoleOffrePlus:
	default:
		return nil, errs.Validation(msgRole)
	}
	var slug string
	if err := s.db.QueryRowContext(ctx, `SELECT slug FROM groups WHERE id = ?`, groupID).Scan(&slug); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, errs.New(errs.KindNotFound, "Ce groupe n'existe pas.")
		}
		return nil, errs.Internal(err)
	}
	p, err := s.parts.Register(ctx, groupID, in, verifiedAt)
	if err != nil {
		return nil, err
	}
	gid, pid := strconv.FormatInt(groupID, 10), strconv.FormatInt(p.ID, 10)
	tok, err := s.creds.IssueLink(ctx, gid, pid)
	if err != nil {
		return nil, err
	}
	code, err := s.creds.IssueCode(ctx, gid, pid)
	if err != nil {
		return nil, err
	}
	return &Created{Participant: p, Link: s.link(slug, tok), Code: code}, nil
}
