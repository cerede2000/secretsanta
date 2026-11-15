package app

import (
	"database/sql"
	"fmt"

	"secretsanta/internal/access/magic"
	accessmig "secretsanta/internal/access/migrations"
	"secretsanta/internal/db"
	"secretsanta/internal/draw"
	"secretsanta/internal/groups"
	"secretsanta/internal/groupsettings"
	"secretsanta/internal/letter"
	mailmig "secretsanta/internal/mail/migrations"
	"secretsanta/internal/mailsuppress"
	unsubmig "secretsanta/internal/mailunsub/migrations"
	"secretsanta/internal/participants"
	"secretsanta/internal/reminderrules"
	"secretsanta/internal/reveal"
	"secretsanta/internal/rulechange"
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
	// letter référence groups et participants : après eux.
	{letter.Module, letter.Migrations},
	// Vague 5. groupsettings, rulechange et draw ne référencent que groups
	// (et leurs propres tables) ; mail (file d'envoi) ne référence aucune autre
	// table. Tous après groups, participants et rules.
	{groupsettings.Module, groupsettings.Migrations},
	{rulechange.Module, rulechange.Migrations},
	{draw.Module, draw.Migrations},
	{mailmig.Module, mailmig.Migrations},
	// Vague 6. mail_unsub ne référence aucune autre table ; reveal référence
	// groups, draws et draw_pairs : après draw.
	{unsubmig.Module, unsubmig.Migrations},
	{reveal.Module, reveal.Migrations},
	// Vague 7. reminderrules référence groups ; mail_suppression est
	// indépendante (adresse et groupe seulement). Après groups.
	{reminderrules.Module, reminderrules.Migrations},
	{mailsuppress.Module, mailsuppress.Migrations},
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
