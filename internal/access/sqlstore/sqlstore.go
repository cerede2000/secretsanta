// Package sqlstore est le magasin SQLite des accès participant : il
// implémente token.Store sur la table access_credentials créée par
// internal/access/migrations.
//
// Il ne voit et ne conserve que des empreintes : le secret en clair (jeton ou
// code) ne lui est jamais transmis.
package sqlstore

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"modernc.org/sqlite"
	sqlite3 "modernc.org/sqlite/lib"

	"secretsanta/internal/access/token"
)

// Store implémente token.Store sur une base SQLite migrée.
type Store struct {
	db *sql.DB
}

var _ token.Store = (*Store)(nil)

// New renvoie un magasin sur d, qui doit avoir reçu les migrations de
// internal/access/migrations.
func New(d *sql.DB) *Store {
	return &Store{db: d}
}

// Put enregistre l'accès c. Dans une seule transaction, il retire le secret
// précédent du même participant pour la même nature (renouvellement), puis
// insère le nouveau. Si l'empreinte appartient déjà à un autre participant du
// groupe pour cette nature, la transaction est annulée (l'ancien secret reste
// valable) et Put renvoie token.ErrDuplicate.
func (s *Store) Put(ctx context.Context, c token.Credential) (err error) {
	if c.GroupID == "" || c.ParticipantID == "" || c.Hash == "" {
		return errors.New("sqlstore: accès incomplet")
	}
	if c.Kind != token.KindLink && c.Kind != token.KindCode {
		return fmt.Errorf("sqlstore: nature inconnue %q", c.Kind)
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()
	// L'écriture vient en premier : la transaction prend aussitôt le verrou
	// d'écriture, ce qui évite l'échec immédiat d'une lecture promue en
	// écriture quand deux émissions se croisent.
	if _, err = tx.ExecContext(ctx,
		`DELETE FROM access_credentials WHERE group_id = ? AND kind = ? AND participant_id = ?`,
		c.GroupID, string(c.Kind), c.ParticipantID); err != nil {
		return err
	}
	var owner string
	switch err = tx.QueryRowContext(ctx,
		`SELECT participant_id FROM access_credentials WHERE group_id = ? AND kind = ? AND token_hash = ?`,
		c.GroupID, string(c.Kind), c.Hash).Scan(&owner); {
	case err == nil:
		// Après le retrait ci-dessus, la ligne appartient forcément à un
		// autre participant.
		err = token.ErrDuplicate
		return err
	case errors.Is(err, sql.ErrNoRows):
		err = nil
	default:
		return err
	}
	if _, err = tx.ExecContext(ctx,
		`INSERT INTO access_credentials (group_id, participant_id, kind, token_hash) VALUES (?, ?, ?, ?)`,
		c.GroupID, c.ParticipantID, string(c.Kind), c.Hash); err != nil {
		if isUniqueViolation(err) {
			err = token.ErrDuplicate
		}
		return err
	}
	err = tx.Commit()
	return err
}

// Find cherche l'empreinte dans le groupe et la nature donnés, et nulle part
// ailleurs. L'accès renvoyé est celui lu dans la ligne (et non l'écho des
// paramètres), pour que le service puisse revérifier groupe, nature et
// empreinte.
func (s *Store) Find(ctx context.Context, groupID string, kind token.Kind, hash string) (token.Credential, bool, error) {
	var c token.Credential
	var k string
	err := s.db.QueryRowContext(ctx,
		`SELECT group_id, participant_id, kind, token_hash FROM access_credentials
		 WHERE group_id = ? AND kind = ? AND token_hash = ?`,
		groupID, string(kind), hash).Scan(&c.GroupID, &c.ParticipantID, &k, &c.Hash)
	c.Kind = token.Kind(k)
	if errors.Is(err, sql.ErrNoRows) {
		return token.Credential{}, false, nil
	}
	if err != nil {
		return token.Credential{}, false, err
	}
	return c, true, nil
}

func isUniqueViolation(err error) bool {
	var se *sqlite.Error
	return errors.As(err, &se) && se.Code() == sqlite3.SQLITE_CONSTRAINT_UNIQUE
}
