package db

import (
	"database/sql"
	"fmt"
	"io/fs"
	"path"
	"sort"
	"strconv"
	"strings"
)

// Migration est une étape SQL ordonnée d'un module.
type Migration struct {
	Version int
	Name    string
	SQL     string
}

// LoadFS lit les fichiers `NNN_nom.sql` à la racine de dir dans fsys (go:embed).
func LoadFS(fsys fs.FS, dir string) ([]Migration, error) {
	entries, err := fs.ReadDir(fsys, dir)
	if err != nil {
		return nil, fmt.Errorf("lecture des migrations : %w", err)
	}
	var out []Migration
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".sql") {
			continue
		}
		base := strings.TrimSuffix(e.Name(), ".sql")
		num, name, _ := strings.Cut(base, "_")
		v, err := strconv.Atoi(num)
		if err != nil || v <= 0 {
			return nil, fmt.Errorf("nom de migration invalide : %s", e.Name())
		}
		b, err := fs.ReadFile(fsys, path.Join(dir, e.Name()))
		if err != nil {
			return nil, err
		}
		out = append(out, Migration{Version: v, Name: name, SQL: string(b)})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Version < out[j].Version })
	return out, nil
}

const trackingDDL = `CREATE TABLE IF NOT EXISTS schema_migrations (
	module TEXT NOT NULL,
	version INTEGER NOT NULL,
	name TEXT NOT NULL,
	applied_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ','now')),
	PRIMARY KEY (module, version)
)`

// Migrate applique dans l'ordre les migrations non encore appliquées du module.
// Chaque migration s'exécute dans sa propre transaction : en cas d'échec elle est
// annulée entièrement et l'erreur est renvoyée. Rejouer est sans effet.
func Migrate(d *sql.DB, module string, migrations []Migration) error {
	if _, err := d.Exec(trackingDDL); err != nil {
		return fmt.Errorf("table de suivi des migrations : %w", err)
	}
	ms := append([]Migration(nil), migrations...)
	sort.Slice(ms, func(i, j int) bool { return ms[i].Version < ms[j].Version })
	for i := 1; i < len(ms); i++ {
		if ms[i].Version == ms[i-1].Version {
			return fmt.Errorf("migration %s : version %d en double", module, ms[i].Version)
		}
	}
	for _, m := range ms {
		if err := applyOne(d, module, m); err != nil {
			return err
		}
	}
	return nil
}

func applyOne(d *sql.DB, module string, m Migration) error {
	tx, err := d.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	var n int
	if err := tx.QueryRow(`SELECT COUNT(*) FROM schema_migrations WHERE module = ? AND version = ?`, module, m.Version).Scan(&n); err != nil {
		return err
	}
	if n > 0 {
		return nil
	}
	if _, err := tx.Exec(m.SQL); err != nil {
		return fmt.Errorf("migration %s %d (%s) : %w", module, m.Version, m.Name, err)
	}
	if _, err := tx.Exec(`INSERT INTO schema_migrations (module, version, name) VALUES (?, ?, ?)`, module, m.Version, m.Name); err != nil {
		return err
	}
	return tx.Commit()
}
