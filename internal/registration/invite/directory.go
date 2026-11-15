package invite

import (
	"context"
	"database/sql"
	"strings"

	"secretsanta/internal/participants"
	"secretsanta/internal/shared/errs"
)

// Directory est la vue des participants d'un groupe dont l'inscription a
// besoin. Elle ne renvoie jamais de nom : seulement des oui/non et un compte.
type Directory interface {
	// NameExists applique la règle d'unicité de F3 (participants.NameExists).
	NameExists(ctx context.Context, groupID int64, firstName, name string) (bool, error)
	// EmailKnown indique si un participant du groupe utilise déjà l'adresse
	// (sans casse).
	EmailKnown(ctx context.Context, groupID int64, email string) (bool, error)
	// Count renvoie le nombre de participants du groupe.
	Count(ctx context.Context, groupID int64) (int, error)
}

// SQLDirectory lit la table participants de F3.
type SQLDirectory struct{ DB *sql.DB }

var _ Directory = SQLDirectory{}

// NameExists délègue à participants.NameExists (aucune copie de la règle).
func (d SQLDirectory) NameExists(ctx context.Context, groupID int64, firstName, name string) (bool, error) {
	return participants.NameExists(ctx, d.DB, groupID, firstName, name, 0)
}

// EmailKnown implémente Directory.
func (d SQLDirectory) EmailKnown(ctx context.Context, groupID int64, email string) (bool, error) {
	key := emailKey(email)
	if key == "" {
		return false, nil
	}
	// Comparaison en Go : lower() de SQLite ne traite que l'ASCII.
	rows, err := d.DB.QueryContext(ctx,
		`SELECT email FROM participants WHERE group_id = ? AND email <> ''`, groupID)
	if err != nil {
		return false, errs.Internal(err)
	}
	defer func() { _ = rows.Close() }()
	found := false
	for rows.Next() {
		var e string
		if err := rows.Scan(&e); err != nil {
			return false, errs.Internal(err)
		}
		if emailKey(e) == key {
			found = true
		}
	}
	if err := rows.Err(); err != nil {
		return false, errs.Internal(err)
	}
	return found, nil
}

// Count implémente Directory.
func (d SQLDirectory) Count(ctx context.Context, groupID int64) (int, error) {
	var n int
	if err := d.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM participants WHERE group_id = ?`, groupID).Scan(&n); err != nil {
		return 0, errs.Internal(err)
	}
	return n, nil
}

// emailKey est la clé de comparaison d'une adresse : bords retirés,
// minuscules.
func emailKey(s string) string { return strings.ToLower(strings.TrimSpace(s)) }
