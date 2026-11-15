// Package migrations embarque les migrations SQL de la liste de suppression.
package migrations

import (
	"embed"

	"secretsanta/internal/db"
)

// Module est le nom de module sous lequel l'orchestrateur enregistre les
// migrations de la liste de suppression.
const Module = "mail_suppression"

//go:embed *.sql
var files embed.FS

// Migrations renvoie les migrations embarquées, à enregistrer par l'orchestrateur.
func Migrations() ([]db.Migration, error) {
	return db.LoadFS(files, ".")
}
