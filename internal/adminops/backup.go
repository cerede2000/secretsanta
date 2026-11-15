package adminops

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"secretsanta/internal/shared/errs"
)

// BackupContentType est le type MIME de la sauvegarde téléchargée.
const BackupContentType = "application/vnd.sqlite3"

// ephemeralTables ne sont jamais copiées dans une sauvegarde : sessions et
// liens de connexion à usage unique ne se restaurent pas.
var ephemeralTables = map[string]bool{"websec_sessions": true, "magic_links": true}

// tableInfo décrit une table de la base pour le filtrage par groupe.
type tableInfo struct {
	name       string
	hasGroupID bool
	hasFK      bool
}

func listTables(ctx context.Context, q interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}) ([]tableInfo, error) {
	rows, err := q.QueryContext(ctx, `SELECT name FROM sqlite_master WHERE type = 'table' AND name NOT LIKE 'sqlite_%' ORDER BY name`)
	if err != nil {
		return nil, err
	}
	var names []string
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			rows.Close()
			return nil, err
		}
		names = append(names, n)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	out := make([]tableInfo, 0, len(names))
	for _, n := range names {
		ti := tableInfo{name: n}
		cols, err := q.QueryContext(ctx, `SELECT name FROM pragma_table_info(?)`, n)
		if err != nil {
			return nil, err
		}
		for cols.Next() {
			var c string
			if err := cols.Scan(&c); err != nil {
				cols.Close()
				return nil, err
			}
			if c == "group_id" {
				ti.hasGroupID = true
			}
		}
		cols.Close()
		r, err := q.QueryContext(ctx, `SELECT COUNT(*) FROM pragma_foreign_key_list(?)`, n)
		if err != nil {
			return nil, err
		}
		var fk int
		if r.Next() {
			err = r.Scan(&fk)
		}
		r.Close()
		if err != nil {
			return nil, err
		}
		ti.hasFK = fk > 0
		out = append(out, ti)
	}
	return out, nil
}

func quoteIdent(s string) string { return `"` + strings.ReplaceAll(s, `"`, `""`) + `"` }

// Backup écrit dans w une copie SQLite cohérente et restaurable qui ne
// contient que le groupe groupID : la base est copiée d'un bloc par
// VACUUM INTO (instantané transactionnel), puis la copie est réduite au
// groupe (les autres groupes et toute ligne rattachée à un autre groupe sont
// retirés, ainsi que les sessions et les liens de connexion), vérifiée
// (integrity_check, foreign_key_check) et compactée. La copie contient le
// tirage du groupe : c'est une sauvegarde complète du groupe.
func (s *Service) Backup(ctx context.Context, groupID int64, w io.Writer) error {
	var one int
	if err := s.db.QueryRowContext(ctx, `SELECT 1 FROM groups WHERE id = ?`, groupID).Scan(&one); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return errs.NotFound("Ce groupe n'existe pas.")
		}
		return errs.Internal(err)
	}
	dir, err := os.MkdirTemp(s.opts.TempDir, "secretsanta-backup-")
	if err != nil {
		return errs.Internal(err)
	}
	defer func() { _ = os.RemoveAll(dir) }()
	path := filepath.Join(dir, "backup.db")
	if _, err := s.db.ExecContext(ctx, `VACUUM INTO ?`, path); err != nil {
		return errs.Internal(fmt.Errorf("copie de la base : %w", err))
	}
	if err := reduceToGroup(ctx, path, groupID); err != nil {
		return errs.Internal(err)
	}
	f, err := os.Open(path)
	if err != nil {
		return errs.Internal(err)
	}
	defer f.Close()
	if _, err := io.Copy(w, f); err != nil {
		return errs.Internal(err)
	}
	if _, err := s.db.ExecContext(ctx, `INSERT INTO adminops_backups(group_id, last_at) VALUES (?, ?)
		ON CONFLICT(group_id) DO UPDATE SET last_at = excluded.last_at`,
		groupID, s.clock.Now().UTC().Format(time.RFC3339)); err != nil {
		return errs.Internal(err)
	}
	return nil
}

// LastBackup renvoie l'instant de la dernière sauvegarde du groupe, nil si
// aucune.
func (s *Service) LastBackup(ctx context.Context, groupID int64) (*time.Time, error) {
	var v string
	err := s.db.QueryRowContext(ctx, `SELECT last_at FROM adminops_backups WHERE group_id = ?`, groupID).Scan(&v)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, errs.Internal(err)
	}
	t, err := time.Parse(time.RFC3339, v)
	if err != nil {
		return nil, errs.Internal(err)
	}
	return &t, nil
}

// reduceToGroup ne garde dans la copie que les données du groupe.
func reduceToGroup(ctx context.Context, path string, groupID int64) error {
	q := url.Values{}
	q.Add("_pragma", "foreign_keys(1)")
	q.Add("_pragma", "journal_mode(DELETE)")
	d, err := sql.Open("sqlite", "file:"+path+"?"+q.Encode())
	if err != nil {
		return err
	}
	defer d.Close()
	d.SetMaxOpenConns(1)
	tables, err := listTables(ctx, d)
	if err != nil {
		return err
	}
	gid := strconv.FormatInt(groupID, 10)
	tx, err := d.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	for _, t := range tables {
		var stmt string
		var args []any
		switch {
		case ephemeralTables[t.name]:
			stmt = `DELETE FROM ` + quoteIdent(t.name)
		case t.name == "groups":
			stmt, args = `DELETE FROM groups WHERE id <> ?`, []any{groupID}
		case t.hasGroupID:
			stmt, args = `DELETE FROM `+quoteIdent(t.name)+` WHERE CAST(group_id AS TEXT) <> ?`, []any{gid}
		default:
			continue // rattachée par clé étrangère (cascade) ou globale
		}
		if _, err := tx.ExecContext(ctx, stmt, args...); err != nil {
			return fmt.Errorf("réduction de %s : %w", t.name, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	var ok string
	if err := d.QueryRowContext(ctx, `PRAGMA integrity_check`).Scan(&ok); err != nil {
		return err
	}
	if ok != "ok" {
		return fmt.Errorf("copie incohérente : %s", ok)
	}
	rows, err := d.QueryContext(ctx, `PRAGMA foreign_key_check`)
	if err != nil {
		return err
	}
	bad := rows.Next()
	rows.Close()
	if bad {
		return errors.New("copie incohérente : clés étrangères")
	}
	if _, err := d.ExecContext(ctx, `VACUUM`); err != nil {
		return err
	}
	return nil
}
