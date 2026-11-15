// Package audit tient le journal des actions sensibles de l'administrateur :
// qui a été concerné, quelle action, quand. Il ne contient jamais de paire ni
// de jeton : une action est un code court, sans texte libre.
package audit

import (
	"context"
	"database/sql"
	"regexp"
	"time"

	"secretsanta/internal/admin/migrations"
	"secretsanta/internal/db"
	"secretsanta/internal/participants"
	"secretsanta/internal/shared/clock"
	"secretsanta/internal/shared/errs"
)

// Module est le nom du module dans schema_migrations.
const Module = migrations.Module

// Migrations renvoie les migrations embarquées, à enregistrer par
// l'orchestrateur après celles de groups.
func Migrations() ([]db.Migration, error) { return migrations.Migrations() }

// Codes d'action de F10.1.
const (
	ActionLinkHandedOver = "lien_remis"
)

// MaxList est le nombre maximal de lignes renvoyées par List.
const MaxList = 200

var actionRe = regexp.MustCompile(`^[a-z0-9_]{1,64}$`)

// Entry est une ligne du journal.
type Entry struct {
	ID            int64
	Action        string
	ParticipantID int64 // 0 : aucune personne concernée
	At            time.Time
}

// Service tient le journal.
type Service struct {
	db  *sql.DB
	clk clock.Clock
}

// New crée le service.
func New(d *sql.DB, c clock.Clock) *Service { return &Service{db: d, clk: c} }

func requireAdmin(a participants.Actor, groupID int64) error {
	if a.Kind != participants.ActorAdmin || a.GroupID != groupID || groupID == 0 {
		return errs.Forbidden("Tu n'as pas le droit d'effectuer cette action.")
	}
	return nil
}

// Record consigne une action sensible de l'administrateur du groupe.
// participantID vaut 0 quand aucune personne n'est concernée.
func (s *Service) Record(ctx context.Context, a participants.Actor, groupID int64, action string, participantID int64) error {
	if err := requireAdmin(a, groupID); err != nil {
		return err
	}
	return s.RecordTx(ctx, s.db, groupID, action, participantID)
}

// Execer est ce que RecordTx demande à *sql.DB ou *sql.Tx.
type Execer interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}

// RecordTx consigne une action dans la transaction de l'appelant : l'action et
// sa trace réussissent ou échouent ensemble. L'appelant a déjà vérifié le
// droit d'agir.
func (s *Service) RecordTx(ctx context.Context, q Execer, groupID int64, action string, participantID int64) error {
	if !actionRe.MatchString(action) {
		return errs.Validation("Le code d'action du journal n'est pas valide.")
	}
	var pid any
	if participantID != 0 {
		pid = participantID
	}
	if _, err := q.ExecContext(ctx, `INSERT INTO admin_audit (group_id, action, participant_id, at) VALUES (?, ?, ?, ?)`,
		groupID, action, pid, s.clk.Now().UTC().UnixNano()); err != nil {
		return errs.Internal(err)
	}
	return nil
}

// List renvoie les dernières lignes du groupe, de la plus récente à la plus
// ancienne (au plus MaxList).
func (s *Service) List(ctx context.Context, a participants.Actor, groupID int64, limit int) ([]Entry, error) {
	if err := requireAdmin(a, groupID); err != nil {
		return nil, err
	}
	if limit <= 0 || limit > MaxList {
		limit = MaxList
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id, action, COALESCE(participant_id, 0), at FROM admin_audit
		WHERE group_id = ? ORDER BY id DESC LIMIT ?`, groupID, limit)
	if err != nil {
		return nil, errs.Internal(err)
	}
	defer rows.Close()
	var out []Entry
	for rows.Next() {
		var e Entry
		var at int64
		if err := rows.Scan(&e.ID, &e.Action, &e.ParticipantID, &at); err != nil {
			return nil, errs.Internal(err)
		}
		e.At = time.Unix(0, at).UTC()
		out = append(out, e)
	}
	if err := rows.Err(); err != nil {
		return nil, errs.Internal(err)
	}
	return out, nil
}
