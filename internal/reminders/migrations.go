package reminders

import (
	"embed"

	"secretsanta/internal/db"
)

// Module est le nom du module dans schema_migrations (après groups, letter et draw).
const Module = "reminders"

//go:embed migrations/*.sql
var migrationsFS embed.FS

// Migrations renvoie les migrations embarquées, à enregistrer par l'orchestrateur.
func Migrations() ([]db.Migration, error) { return db.LoadFS(migrationsFS, "migrations") }
