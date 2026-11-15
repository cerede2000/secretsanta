package draw

import (
	"context"
	"database/sql"
	"errors"

	"secretsanta/internal/shared/errs"
	"secretsanta/internal/shared/ports"
)

// State implémente le port ports.DrawState. Il ne dépend que de la base :
// l'orchestrateur peut le construire avant participants (qui en a besoin).
type State struct{ db *sql.DB }

// NewState construit le port DrawState.
func NewState(d *sql.DB) *State { return &State{db: d} }

var _ ports.DrawState = (*State)(nil)

// IsDrawDone indique si le groupe a un tirage actuel (fait, refait ou
// ajusté).
func (s *State) IsDrawDone(ctx context.Context, groupID int64) (bool, error) {
	st, err := s.Status(ctx, groupID)
	if err != nil {
		return false, err
	}
	return st.Done, nil
}

// Status renvoie l'état du tirage actuel, sans aucune paire.
func (s *State) Status(ctx context.Context, groupID int64) (Status, error) {
	return currentStatus(ctx, s.db, groupID)
}

type queryRower interface {
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

func currentStatus(ctx context.Context, q queryRower, groupID int64) (Status, error) {
	var st Status
	var drawn string
	err := q.QueryRowContext(ctx, `SELECT id, year, kind, revision, drawn_at FROM draws
		WHERE group_id = ? AND status = 'actuel'`, groupID).Scan(&st.DrawID, &st.Year, &st.Kind, &st.Revision, &drawn)
	if errors.Is(err, sql.ErrNoRows) {
		return Status{}, nil
	}
	if err != nil {
		return Status{}, errs.Internal(err)
	}
	t, err := parseTime(drawn)
	if err != nil {
		return Status{}, errs.Internal(err)
	}
	st.DrawnAt = t
	st.Done = true
	return st, nil
}
