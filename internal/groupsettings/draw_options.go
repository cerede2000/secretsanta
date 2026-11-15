package groupsettings

import (
	"context"
	"database/sql"
	"errors"

	"secretsanta/internal/shared/errs"
)

// DrawOptions sont les options du tirage d'un groupe. Elles ne valent que pour
// les tirages à venir : les changer ne touche jamais une paire existante.
type DrawOptions struct {
	// ExcludePreviousYear interdit les paires du tirage de l'année précédente.
	ExcludePreviousYear bool
	// AntiSwap interdit qu'une personne A offre à B quand B offre à A.
	AntiSwap bool
}

// SetDrawOptions enregistre les options du tirage du groupe (modifiables à
// tout moment, y compris après le tirage : elles servent au prochain).
func (s *Service) SetDrawOptions(ctx context.Context, groupID int64, o DrawOptions) error {
	if _, err := s.group(ctx, groupID); err != nil {
		return err
	}
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO group_draw_options (group_id, exclude_previous_year, anti_swap) VALUES (?, ?, ?)
		 ON CONFLICT(group_id) DO UPDATE SET exclude_previous_year = excluded.exclude_previous_year,
		 anti_swap = excluded.anti_swap`, groupID, boolInt(o.ExcludePreviousYear), boolInt(o.AntiSwap))
	if err != nil {
		return errs.Internal(err)
	}
	return nil
}

// GetDrawOptions relit les options du tirage ; une ligne absente donne les
// deux options désactivées.
func (s *Service) GetDrawOptions(ctx context.Context, groupID int64) (DrawOptions, error) {
	var ex, sw int
	err := s.db.QueryRowContext(ctx,
		`SELECT exclude_previous_year, anti_swap FROM group_draw_options WHERE group_id = ?`, groupID).Scan(&ex, &sw)
	if errors.Is(err, sql.ErrNoRows) {
		return DrawOptions{}, nil
	}
	if err != nil {
		return DrawOptions{}, errs.Internal(err)
	}
	return DrawOptions{ExcludePreviousYear: ex == 1, AntiSwap: sw == 1}, nil
}

// DrawOptions implémente draw.OptionSource.
func (s *Service) DrawOptions(ctx context.Context, groupID int64) (excludePreviousYear, antiSwap bool, err error) {
	o, err := s.GetDrawOptions(ctx, groupID)
	return o.ExcludePreviousYear, o.AntiSwap, err
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
