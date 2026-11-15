package draw

import (
	"context"
	"database/sql"
	"errors"

	"secretsanta/internal/shared/errs"
)

// AdjustTx remplace les paires du tirage actuel par pairs, dans la
// transaction de l'appelant (F4c : ajustement, réparation). Les paires
// inchangées gardent leur drapeau de découverte ; la révision augmente et
// l'ajustement est consigné dans l'historique. Seule la cohérence
// structurelle est vérifiée ici (chaque destinataire une seule fois, personne
// ne s'offre son cadeau) ; les règles sont l'affaire de l'appelant.
func (s *Service) AdjustTx(ctx context.Context, tx *sql.Tx, groupID int64, pairs []Pair) error {
	if len(pairs) == 0 {
		return errs.Validation(msgAdjustInvalid)
	}
	seen := map[int64]bool{}
	want := map[Pair]bool{}
	for _, p := range pairs {
		if p.Giver == p.Receiver || seen[p.Receiver] {
			return errs.Validation(msgAdjustInvalid)
		}
		seen[p.Receiver] = true
		want[p] = true
	}
	var drawID int64
	var rev int
	err := tx.QueryRowContext(ctx, `SELECT id, revision FROM draws WHERE group_id = ? AND status = 'actuel'`, groupID).
		Scan(&drawID, &rev)
	if errors.Is(err, sql.ErrNoRows) {
		return errNotDone()
	}
	if err != nil {
		return internal(err)
	}
	rows, err := tx.QueryContext(ctx, `SELECT id, giver_id, receiver_id FROM draw_pairs WHERE draw_id = ?`, drawID)
	if err != nil {
		return internal(err)
	}
	have := map[Pair]bool{}
	var drop []int64
	for rows.Next() {
		var id int64
		var p Pair
		if err := rows.Scan(&id, &p.Giver, &p.Receiver); err != nil {
			rows.Close()
			return internal(err)
		}
		if want[p] {
			have[p] = true
		} else {
			drop = append(drop, id)
		}
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return internal(err)
	}
	rows.Close()
	for _, id := range drop {
		if _, err := tx.ExecContext(ctx, `DELETE FROM draw_pairs WHERE id = ?`, id); err != nil {
			return internal(err)
		}
	}
	var add []Pair
	for _, p := range pairs {
		if !have[p] {
			add = append(add, p)
		}
	}
	if err := insertPairs(ctx, tx, drawID, add); err != nil {
		return err
	}
	rev++
	if _, err := tx.ExecContext(ctx, `UPDATE draws SET revision = ? WHERE id = ?`, rev, drawID); err != nil {
		return internal(err)
	}
	return insertEvent(ctx, tx, groupID, drawID, KindAdjustment, rev, formatTime(s.clock.Now()))
}
