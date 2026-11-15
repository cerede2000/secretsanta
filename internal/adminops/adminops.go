// Package adminops porte les outils d'exploitation de l'administration
// (F10b.2, F10b.3) : export et import JSON atomiques des participants (rôle
// dans le tirage compris), sauvegarde SQLite cohérente du groupe,
// réinitialisation du secret d'administration, duplication d'un groupe pour
// l'année suivante et suppression avec purge facultative.
//
// Le paquet ne monte aucune route lui-même : l'orchestrateur appelle
// Pages.Register et enregistre Migrations sous Module, après groups.
package adminops

import (
	"database/sql"
	"embed"
	"errors"

	"secretsanta/internal/db"
	"secretsanta/internal/shared/clock"
)

// Module est le nom du module dans schema_migrations.
const Module = "adminops"

//go:embed migrations/*.sql
var migrationsFS embed.FS

// Migrations renvoie les migrations embarquées du module (après groups).
func Migrations() ([]db.Migration, error) {
	return db.LoadFS(migrationsFS, "migrations")
}

// Options configure le service.
type Options struct {
	// ImageDir est le dossier des images des lettres (letter.NewService) :
	// la purge d'un groupe efface <ImageDir>/<id du groupe>. Vide : aucune
	// image à effacer.
	ImageDir string
	// TempDir reçoit la copie temporaire d'une sauvegarde (dossier
	// temporaire du système si vide).
	TempDir string
}

// Service regroupe les opérations d'exploitation.
type Service struct {
	db    *sql.DB
	clock clock.Clock
	opts  Options
}

// NewService construit le service.
func NewService(d *sql.DB, c clock.Clock, opts Options) (*Service, error) {
	if d == nil || c == nil {
		return nil, errors.New("adminops : base et horloge obligatoires")
	}
	return &Service{db: d, clock: c, opts: opts}, nil
}
