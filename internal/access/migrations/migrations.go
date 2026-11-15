// Package migrations embarque les migrations SQL des accès participant
// (jetons de lien personnel et codes courts), que l'orchestrateur enregistre
// dans le registre commun sous le module Module.
package migrations

import (
	"embed"

	"secretsanta/internal/db"
)

// Module est le nom de module sous lequel db.Migrate suit les versions des
// migrations d'accès.
const Module = "access_credentials"

//go:embed *.sql
var fsys embed.FS

// Migrations renvoie les migrations embarquées, triées par version.
func Migrations() ([]db.Migration, error) {
	return db.LoadFS(fsys, ".")
}
