package websec

import (
	"embed"

	"secretsanta/internal/db"
)

// MigrationModule est le nom du module dans schema_migrations.
const MigrationModule = "websec"

//go:embed migrations/*.sql
var migrationsFS embed.FS

// Migrations renvoie les migrations embarquées du module, à enregistrer par
// l'orchestrateur dans le registre commun.
func Migrations() ([]db.Migration, error) {
	return db.LoadFS(migrationsFS, "migrations")
}
