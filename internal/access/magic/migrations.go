package magic

import (
	"embed"

	"secretsanta/internal/db"
)

// Module est le nom de module sous lequel l'orchestrateur enregistre les
// migrations du lien magique (db.Migrate suit les versions par module).
const Module = "access_magic"

//go:embed migrations/*.sql
var migrationsFS embed.FS

// Migrations renvoie les migrations embarquées du lien magique, à enregistrer
// par l'orchestrateur dans le registre commun.
func Migrations() ([]db.Migration, error) {
	return db.LoadFS(migrationsFS, "migrations")
}
