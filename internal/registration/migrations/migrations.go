// Package migrations embarque les migrations SQL de l'inscription par lien
// (F11.6) : invitation, inscriptions en attente et codes de vérification.
// L'orchestrateur les enregistre dans le registre commun sous le module
// Module, après celles des groupes (clés étrangères vers groups).
package migrations

import (
	"embed"

	"secretsanta/internal/db"
)

// Module est le nom de module sous lequel db.Migrate suit les versions des
// migrations de l'inscription.
const Module = "registration"

//go:embed *.sql
var fsys embed.FS

// Migrations renvoie les migrations embarquées, triées par version.
func Migrations() ([]db.Migration, error) {
	return db.LoadFS(fsys, ".")
}
