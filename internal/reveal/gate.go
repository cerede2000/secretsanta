package reveal

import (
	"context"
	"database/sql"

	"secretsanta/internal/draw"
	"secretsanta/internal/shared/clock"
)

// Gate est la porte de révélation à donner au service du tirage
// (draw.Options.Gate). Elle est ouverte pour le tirage actuel quand une
// ouverture est enregistrée (« ouvrir maintenant », tâche planifiée), quand
// le groupe n'a pas de date de révélation, ou quand l'horloge a atteint la
// date. Sans tirage, elle est fermée. Elle ne dépend que de la base et de
// l'horloge : l'orchestrateur la construit avant le service du tirage.
type Gate struct {
	db    *sql.DB
	clock clock.Clock
}

var _ draw.RevealGate = (*Gate)(nil)

// NewGate construit la porte de révélation.
func NewGate(d *sql.DB, c clock.Clock) *Gate { return &Gate{db: d, clock: c} }

// IsOpen dit si les cibles du tirage actuel peuvent être renvoyées à leurs
// tireurs.
func (g *Gate) IsOpen(ctx context.Context, groupID int64) (bool, error) {
	st, err := readState(ctx, g.db, g.clock.Now(), groupID)
	if err != nil {
		return false, err
	}
	return st.draw.ok && st.open, nil
}
