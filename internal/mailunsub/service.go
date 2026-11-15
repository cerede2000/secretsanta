package mailunsub

import (
	"context"
	"database/sql"
	"errors"
	"net/url"
	"strings"

	"secretsanta/internal/mail/templates"
	"secretsanta/internal/shared/clock"
)

// RoutePrefix est le chemin des liens de désinscription : /desinscription/{jeton}.
const RoutePrefix = "/desinscription/"

// Options rassemble les dépendances du service.
type Options struct {
	DB         *sql.DB
	Clock      clock.Clock
	SigningKey []byte // config.SigningKey.Reveal()
	PublicURL  string // PUBLIC_URL, base des liens
}

// Service tient les préférences de désinscription.
type Service struct {
	db     *sql.DB
	clk    clock.Clock
	signer *Signer
	base   string
}

// NewService crée le service.
func NewService(o Options) (*Service, error) {
	if o.DB == nil || o.Clock == nil || len(o.SigningKey) == 0 || o.PublicURL == "" {
		return nil, errors.New("mailunsub: dépendance manquante")
	}
	if _, err := url.Parse(o.PublicURL); err != nil {
		return nil, err
	}
	return &Service{db: o.DB, clk: o.Clock, signer: NewSigner(o.SigningKey), base: strings.TrimRight(o.PublicURL, "/")}, nil
}

// Signer expose le signataire (pour les gestionnaires).
func (s *Service) Signer() *Signer { return s.signer }

// IsUnsubscribed dit si le participant a coupé ce type. Un type inconnu ou
// essentiel n'est jamais coupé.
func (s *Service) IsUnsubscribed(ctx context.Context, participantID int64, mailType string) (bool, error) {
	if !ValidType(mailType) {
		return false, nil
	}
	var n int
	err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM mail_unsubscribe WHERE participant_id = ? AND mail_type = ?`,
		participantID, mailType).Scan(&n)
	return n > 0, err
}

// Unsubscribe coupe un type pour un participant (idempotent).
func (s *Service) Unsubscribe(ctx context.Context, groupID, participantID int64, mailType string) error {
	if !ValidType(mailType) {
		return ErrUnknownType
	}
	_, err := s.db.ExecContext(ctx,
		`INSERT OR IGNORE INTO mail_unsubscribe (participant_id, mail_type, group_id, created_at) VALUES (?, ?, ?, ?)`,
		participantID, mailType, groupID, s.clk.Now().UnixNano())
	return err
}

// Resubscribe rétablit un type (idempotent).
func (s *Service) Resubscribe(ctx context.Context, participantID int64, mailType string) error {
	if !ValidType(mailType) {
		return ErrUnknownType
	}
	_, err := s.db.ExecContext(ctx,
		`DELETE FROM mail_unsubscribe WHERE participant_id = ? AND mail_type = ?`, participantID, mailType)
	return err
}

// ShouldSend dit si un mail de ce modèle peut partir : toujours vrai pour un
// mail essentiel, quelles que soient les désinscriptions.
func (s *Service) ShouldSend(ctx context.Context, kind templates.Kind, participantID int64) (bool, error) {
	info, ok := templates.InfoOf(kind)
	if !ok {
		return false, templates.ErrData
	}
	if info.Essential || info.UnsubType == "" {
		return true, nil
	}
	off, err := s.IsUnsubscribed(ctx, participantID, info.UnsubType)
	return !off, err
}

// URL renvoie le lien signé de désinscription d'un type.
func (s *Service) URL(groupID, participantID int64, mailType string) (string, error) {
	tok, err := s.signer.Sign(Claims{GroupID: groupID, ParticipantID: participantID, Type: mailType})
	if err != nil {
		return "", err
	}
	return s.base + RoutePrefix + tok, nil
}

// Decorate complète les données d'un modèle : lien signé pour un modèle non
// essentiel, aucun lien pour un mail essentiel (même si d en portait un).
func (s *Service) Decorate(kind templates.Kind, groupID, participantID int64, d templates.Data) (templates.Data, error) {
	info, ok := templates.InfoOf(kind)
	if !ok {
		return d, templates.ErrData
	}
	if info.Essential || info.UnsubType == "" {
		d.UnsubscribeURL, d.UnsubscribeMailto = "", ""
		return d, nil
	}
	u, err := s.URL(groupID, participantID, info.UnsubType)
	if err != nil {
		return d, err
	}
	d.UnsubscribeURL = u
	return d, nil
}
