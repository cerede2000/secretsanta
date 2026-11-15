package adminops

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"

	"secretsanta/internal/shared/errs"
	"secretsanta/internal/shared/sanitize"
)

// CodeDeleteConfirm est le code du refus quand la confirmation ne correspond
// pas au nom du groupe.
const CodeDeleteConfirm = "adminops.error.delete_confirm"

// DeleteInput est la demande de suppression d'un groupe.
type DeleteInput struct {
	// ConfirmName doit reprendre le nom du groupe (confirmation intégrée,
	// jamais une boîte du navigateur).
	ConfirmName string
	// Purge efface aussi tout ce qui reste rattaché au groupe hors des
	// tables liées par clé étrangère : historique des courriels, liste de
	// suppression, désinscriptions et images des lettres sur disque.
	Purge bool
}

// DeleteGroup supprime le groupe après confirmation par son nom.
//
// Sans purge : le groupe et tout ce qui en dépend par clé étrangère
// (participants, règles, tirages, lettres, inscriptions, réglages…) sont
// supprimés, ainsi que les accès (sessions, liens de connexion, liens et
// codes personnels) et les courriels encore en attente d'envoi ; l'historique
// des courriels envoyés, la liste de suppression et les désinscriptions
// restent. Avec purge : toute ligne de toute table portant l'identifiant du
// groupe est effacée, ainsi que le dossier des images des lettres du groupe.
// Les autres groupes ne sont jamais touchés.
func (s *Service) DeleteGroup(ctx context.Context, groupID int64, in DeleteInput) error {
	confirm, _ := sanitize.Line(in.ConfirmName, 200, "La confirmation")
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return errs.Internal(err)
	}
	defer func() { _ = tx.Rollback() }()
	var name string
	if err := tx.QueryRowContext(ctx, `SELECT name FROM groups WHERE id = ?`, groupID).Scan(&name); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return errs.NotFound("Ce groupe n'existe pas.")
		}
		return errs.Internal(err)
	}
	if confirm == "" || confirm != name {
		return errs.Validation("Tape exactement le nom du groupe pour confirmer la suppression.").WithCode(CodeDeleteConfirm)
	}
	gid := strconv.FormatInt(groupID, 10)
	stmts := []struct {
		q    string
		args []any
	}{
		{`DELETE FROM websec_sessions WHERE group_id = ?`, []any{gid}},
		{`DELETE FROM access_credentials WHERE group_id = ?`, []any{gid}},
		{`DELETE FROM magic_links WHERE group_id = ?`, []any{groupID}},
		{`DELETE FROM mail_queue WHERE group_id = ? AND status IN ('pending', 'sending')`, []any{groupID}},
	}
	if in.Purge {
		tables, err := listTables(ctx, tx)
		if err != nil {
			return errs.Internal(err)
		}
		stmts = stmts[:0]
		for _, t := range tables {
			if t.hasGroupID {
				stmts = append(stmts, struct {
					q    string
					args []any
				}{`DELETE FROM ` + quoteIdent(t.name) + ` WHERE CAST(group_id AS TEXT) = ?`, []any{gid}})
			}
		}
	}
	for _, st := range stmts {
		if _, err := tx.ExecContext(ctx, st.q, st.args...); err != nil {
			return errs.Internal(fmt.Errorf("suppression : %w", err))
		}
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM groups WHERE id = ?`, groupID); err != nil {
		return errs.Internal(err)
	}
	if err := tx.Commit(); err != nil {
		return errs.Internal(err)
	}
	if in.Purge && s.opts.ImageDir != "" {
		if err := os.RemoveAll(filepath.Join(s.opts.ImageDir, gid)); err != nil {
			return errs.Internal(err)
		}
	}
	return nil
}
