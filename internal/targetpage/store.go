package targetpage

import (
	"context"
	"database/sql"
	"embed"
	"errors"

	"secretsanta/internal/db"
)

// Module est le nom du module dans schema_migrations. Ses migrations
// s'appliquent après celles de groups et de participants.
const Module = "targetpage"

//go:embed migrations/*.sql
var migrationsFS embed.FS

// Migrations renvoie les migrations embarquées, à enregistrer par
// l'orchestrateur.
func Migrations() ([]db.Migration, error) {
	return db.LoadFS(migrationsFS, "migrations")
}

// Store mémorise, par participant, la dernière version du budget vue.
type Store struct{ db *sql.DB }

// NewStore construit le magasin.
func NewStore(d *sql.DB) *Store { return &Store{db: d} }

// BudgetSeen renvoie la version du budget déjà vue (0 si aucune).
func (s *Store) BudgetSeen(ctx context.Context, groupID, participantID int64) (int, error) {
	var v int
	err := s.db.QueryRowContext(ctx,
		`SELECT version FROM targetpage_budget_seen WHERE group_id = ? AND participant_id = ?`,
		groupID, participantID).Scan(&v)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil
	}
	return v, err
}

// MarkBudgetSeen retient que la version a été vue ; elle ne recule jamais.
func (s *Store) MarkBudgetSeen(ctx context.Context, groupID, participantID int64, version int) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO targetpage_budget_seen (group_id, participant_id, version)
		VALUES (?, ?, ?)
		ON CONFLICT (group_id, participant_id) DO UPDATE SET version = MAX(version, excluded.version)`,
		groupID, participantID, version)
	return err
}
