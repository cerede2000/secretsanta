package app

import (
	"database/sql"
	"fmt"

	"secretsanta/internal/access/magic"
	accessmig "secretsanta/internal/access/migrations"
	"secretsanta/internal/db"
	"secretsanta/internal/groups"
	"secretsanta/internal/participants"
	"secretsanta/internal/rules"
	"secretsanta/internal/websec"
)

// ModuleMigrations rassemble les migrations d'un module.
type ModuleMigrations struct {
	Module     string
	Migrations []db.Migration
}

// migrationSources est le registre ordonné des modules : UNE ligne par module.
// L'ordre est celui d'application ; ajouter les modules des vagues suivantes à
// la fin, jamais au milieu.
var migrationSources = []struct {
	module string
	load   func() ([]db.Migration, error)
}{
	{groups.Module, groups.Migrations},
	{websec.MigrationModule, websec.Migrations},
	{magic.Module, magic.Migrations},
	// Dépendances : participants et rules référencent groups ; rules vient
	// après participants (membres de foyers, exclusions).
	{participants.Module, participants.Migrations},
	{rules.Module, rules.Migrations},
	{accessmig.Module, accessmig.Migrations},
}

// Migrations renvoie le registre ordonné des migrations de tous les modules.
func Migrations() ([]ModuleMigrations, error) {
	seen := map[string]bool{}
	out := make([]ModuleMigrations, 0, len(migrationSources))
	for _, s := range migrationSources {
		if seen[s.module] {
			return nil, fmt.Errorf("module de migrations en double : %s", s.module)
		}
		seen[s.module] = true
		ms, err := s.load()
		if err != nil {
			return nil, fmt.Errorf("migrations %s : %w", s.module, err)
		}
		out = append(out, ModuleMigrations{Module: s.module, Migrations: ms})
	}
	return out, nil
}

// Migrate applique le registre complet sur la base ; rejouable sans effet.
func Migrate(d *sql.DB) error {
	reg, err := Migrations()
	if err != nil {
		return err
	}
	for _, m := range reg {
		if err := db.Migrate(d, m.Module, m.Migrations); err != nil {
			return fmt.Errorf("module %s : %w", m.Module, err)
		}
	}
	return nil
}
