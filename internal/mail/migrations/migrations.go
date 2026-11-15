// Package migrations embarque les migrations SQL du module courrier.
package migrations

import (
	"embed"

	"secretsanta/internal/db"
)

// Module est le nom de module sous lequel l'orchestrateur enregistre les
// migrations du courrier (db.Migrate suit les versions par module).
const Module = "mail"

//go:embed *.sql
var files embed.FS

// Migrations renvoie les migrations embarquées, à enregistrer par l'orchestrateur.
func Migrations() ([]db.Migration, error) {
	return db.LoadFS(files, ".")
}
