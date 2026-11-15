// Package mailsuppress implémente le port ports.SuppressionList : la liste
// des adresses refusées définitivement, séparée par groupe.
package mailsuppress

import (
	"context"
	"database/sql"
	"strings"
	"time"
	"unicode/utf8"

	"secretsanta/internal/shared/clock"
	"secretsanta/internal/shared/errs"
	"secretsanta/internal/shared/ports"
	"secretsanta/internal/shared/sanitize"
)

const maxReasonRunes = 300

// Entry est une adresse supprimée, telle que la page l'affiche.
type Entry struct {
	Address string
	Reason  string
	At      time.Time
}

// Store est la liste de suppression persistante.
type Store struct {
	db    *sql.DB
	clock clock.Clock
}

var _ ports.SuppressionList = (*Store)(nil)

// New construit la liste ; l'heure vient de l'horloge injectée.
func New(db *sql.DB, c clock.Clock) *Store { return &Store{db: db, clock: c} }

func norm(a string) string { return strings.ToLower(strings.TrimSpace(a)) }

func cleanReason(r string) string {
	r, _ = sanitize.Line(r, 1<<20, "La raison")
	if rs := []rune(r); len(rs) > maxReasonRunes {
		r = string(rs[:maxReasonRunes]) + "…"
	}
	if !utf8.ValidString(r) {
		return ""
	}
	return r
}

func internal(err error) error { return errs.Internal(err) }

// IsSuppressed indique si l'adresse est supprimée pour ce groupe seulement.
func (s *Store) IsSuppressed(ctx context.Context, groupID int64, address string) (bool, error) {
	var one int
	err := s.db.QueryRowContext(ctx,
		`SELECT 1 FROM mail_suppression WHERE group_id = ? AND address = ?`,
		groupID, norm(address)).Scan(&one)
	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, internal(err)
	}
	return true, nil
}

// Add inscrit l'adresse avec sa raison (nettoyée) et la date courante. Une
// nouvelle inscription met à jour la raison et la date.
func (s *Store) Add(ctx context.Context, groupID int64, address, reason string) error {
	a := norm(address)
	if a == "" {
		return errs.Validation("L'adresse est obligatoire.")
	}
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO mail_suppression (group_id, address, reason, created_at) VALUES (?, ?, ?, ?)
		 ON CONFLICT (group_id, address) DO UPDATE SET reason = excluded.reason, created_at = excluded.created_at`,
		groupID, a, cleanReason(reason), s.clock.Now().UTC().UnixNano())
	if err != nil {
		return internal(err)
	}
	return nil
}

// Remove réactive l'adresse pour ce groupe ; sans effet si elle n'y est pas.
func (s *Store) Remove(ctx context.Context, groupID int64, address string) error {
	_, err := s.db.ExecContext(ctx,
		`DELETE FROM mail_suppression WHERE group_id = ? AND address = ?`, groupID, norm(address))
	if err != nil {
		return internal(err)
	}
	return nil
}

// List renvoie les adresses supprimées du groupe, la plus récente d'abord.
func (s *Store) List(ctx context.Context, groupID int64) ([]Entry, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT address, reason, created_at FROM mail_suppression
		 WHERE group_id = ? ORDER BY created_at DESC, address`, groupID)
	if err != nil {
		return nil, internal(err)
	}
	defer rows.Close()
	var out []Entry
	for rows.Next() {
		var e Entry
		var ns int64
		if err := rows.Scan(&e.Address, &e.Reason, &ns); err != nil {
			return nil, internal(err)
		}
		e.At = time.Unix(0, ns).UTC()
		out = append(out, e)
	}
	if err := rows.Err(); err != nil {
		return nil, internal(err)
	}
	return out, nil
}
