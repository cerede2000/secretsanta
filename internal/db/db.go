// Package db ouvre la base SQLite (pur Go, sans CGO) et applique les migrations embarquées.
package db

import (
	"database/sql"
	"fmt"
	"net/url"
	"os"
	"path/filepath"

	_ "modernc.org/sqlite"
)

// FileName est le nom du fichier de base dans DATA_DIR.
const FileName = "secretsanta.db"

// Open ouvre (et crée si besoin) la base dans dataDir, en mode WAL avec clés étrangères actives.
func Open(dataDir string) (*sql.DB, error) {
	if err := os.MkdirAll(dataDir, 0o750); err != nil {
		return nil, fmt.Errorf("création du dossier de données : %w", err)
	}
	return openDSN(filepath.Join(dataDir, FileName))
}

func openDSN(path string) (*sql.DB, error) {
	q := url.Values{}
	q.Add("_pragma", "journal_mode(WAL)")
	q.Add("_pragma", "foreign_keys(1)")
	q.Add("_pragma", "busy_timeout(5000)")
	d, err := sql.Open("sqlite", "file:"+path+"?"+q.Encode())
	if err != nil {
		return nil, fmt.Errorf("ouverture de la base : %w", err)
	}
	if err := d.Ping(); err != nil {
		_ = d.Close()
		return nil, fmt.Errorf("connexion à la base : %w", err)
	}
	return d, nil
}
