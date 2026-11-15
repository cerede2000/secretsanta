// Package migrations embarque les migrations SQL de l'espace d'administration
// (journal des actions sensibles), que l'orchestrateur enregistre dans le
// registre commun sous le module Module, après groups.
package migrations

import (
	"embed"

	"secretsanta/internal/db"
)

// Module est le nom de module sous lequel db.Migrate suit les versions.
const Module = "admin"

//go:embed *.sql
var fsys embed.FS

// Migrations renvoie les migrations embarquées, triées par version.
func Migrations() ([]db.Migration, error) {
	return db.LoadFS(fsys, ".")
}
