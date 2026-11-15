package reminders

import (
	"context"
	"database/sql"
	"time"

	"secretsanta/internal/shared/clock"
	"secretsanta/internal/shared/errs"
)

// Facts donne les faits qui arrêtent une relance : la personne a agi.
type Facts interface {
	// LinkOpened : la personne a ouvert son lien.
	LinkOpened(ctx context.Context, groupID, participantID int64) (bool, error)
	// LetterWritten : la personne a écrit sa lettre.
	LetterWritten(ctx context.Context, groupID, participantID int64) (bool, error)
	// UnreadGivers renvoie les tireurs du tirage actuel qui n'ont pas ouvert
	// toutes leurs cibles (retardataires de révélation).
	UnreadGivers(ctx context.Context, groupID int64) (map[int64]bool, error)
}

// SQLFacts lit les faits en base (tables letters et draw_pairs en lecture,
// reminder_link_opens en écriture).
type SQLFacts struct {
	DB    *sql.DB
	Clock clock.Clock
}

// RecordLinkOpened note l'ouverture du lien (une seule fois, idempotent). Le
// câblage l'appelle à la première connexion par lien personnel.
func (f SQLFacts) RecordLinkOpened(ctx context.Context, groupID, participantID int64) error {
	_, err := f.DB.ExecContext(ctx, `INSERT OR IGNORE INTO reminder_link_opens (group_id, participant_id, opened_at)
		VALUES (?, ?, ?)`, groupID, participantID, f.Clock.Now().UTC().Format(time.RFC3339))
	if err != nil {
		return errs.Internal(err)
	}
	return nil
}

// LinkOpened implémente Facts.
func (f SQLFacts) LinkOpened(ctx context.Context, groupID, participantID int64) (bool, error) {
	return f.exists(ctx, `SELECT 1 FROM reminder_link_opens WHERE group_id = ? AND participant_id = ?`, groupID, participantID)
}

// LetterWritten implémente Facts : une ligne existe dès la première écriture.
func (f SQLFacts) LetterWritten(ctx context.Context, groupID, participantID int64) (bool, error) {
	return f.exists(ctx, `SELECT 1 FROM letters WHERE group_id = ? AND participant_id = ?`, groupID, participantID)
}

func (f SQLFacts) exists(ctx context.Context, q string, args ...any) (bool, error) {
	var one int
	err := f.DB.QueryRowContext(ctx, q, args...).Scan(&one)
	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, errs.Internal(err)
	}
	return true, nil
}

// UnreadGivers implémente Facts.
func (f SQLFacts) UnreadGivers(ctx context.Context, groupID int64) (map[int64]bool, error) {
	rows, err := f.DB.QueryContext(ctx, `SELECT DISTINCT p.giver_id FROM draw_pairs p
		JOIN draws d ON d.id = p.draw_id
		WHERE d.group_id = ? AND d.status = 'actuel' AND p.discovered_at IS NULL`, groupID)
	if err != nil {
		return nil, errs.Internal(err)
	}
	defer rows.Close()
	out := map[int64]bool{}
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, errs.Internal(err)
		}
		out[id] = true
	}
	if err := rows.Err(); err != nil {
		return nil, errs.Internal(err)
	}
	return out, nil
}
