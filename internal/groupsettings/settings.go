// Package groupsettings porte les réglages du groupe modifiables à tout
// moment, y compris après le tirage (F2e) : nom, budget facultatif, date du
// jour J, consigne globale, plafond de cadeaux par tireur et langue par
// défaut. Le service n'écrit que les données du groupe : jamais une paire.
package groupsettings

import (
	"context"
	"database/sql"
	"embed"
	"errors"
	"strconv"
	"time"

	"secretsanta/internal/db"
	"secretsanta/internal/groups"
	"secretsanta/internal/shared/clock"
	"secretsanta/internal/shared/errs"
	"secretsanta/internal/shared/ports"
	"secretsanta/internal/shared/sanitize"
)

// Module est le nom du module dans schema_migrations.
const Module = "groupsettings"

// EventBudgetChanged est le nom de l'événement émis à chaque changement de budget.
const EventBudgetChanged = "budget_modifie"

// BudgetNone est la valeur du budget dans l'événement quand il n'y en a pas.
const BudgetNone = "aucun"

//go:embed migrations/*.sql
var migrationsFS embed.FS

// Migrations renvoie les migrations embarquées (après celles de groups).
func Migrations() ([]db.Migration, error) {
	return db.LoadFS(migrationsFS, "migrations")
}

const (
	msgEventDate    = "La date du jour J n'est pas valide (format attendu : AAAA-MM-JJ)."
	msgMaxGiftsOut  = "Le plafond de cadeaux par personne doit être compris entre 2 et 5."
	msgMaxGiftsConf = "Un plafond supérieur à 2 cadeaux par personne doit être confirmé explicitement."
	msgNotFound     = "Ce groupe n'existe pas."
	msgLangUnknown  = "Cette langue n'est pas disponible."
)

// Languages indique les langues chargées (satisfait par *catalog.Catalog).
type Languages interface {
	Has(lang string) bool
}

// Service gère les réglages d'un groupe.
type Service struct {
	db    *sql.DB
	clock clock.Clock
	bus   ports.EventBus
	draw  ports.DrawState
	langs Languages
}

// NewService construit le service. bus, draw et langs peuvent être nil : sans
// bus aucun événement n'est émis ; sans langs, tout réglage de langue est refusé.
func NewService(d *sql.DB, c clock.Clock, bus ports.EventBus, draw ports.DrawState, langs Languages) *Service {
	return &Service{db: d, clock: c, bus: bus, draw: draw, langs: langs}
}

// Input est la saisie des réglages. Les réglages sont modifiables après le
// tirage : DrawState n'est jamais un obstacle.
type Input struct {
	GroupID int64
	Name    string
	// Budget en euros, vide pour retirer le budget.
	Budget string
	// EventDate au format AAAA-MM-JJ, vide pour la retirer.
	EventDate        string
	GiftInstructions string
	// MaxGiftsPerGiver nil laisse le plafond inchangé.
	MaxGiftsPerGiver *int
	// ConfirmMaxGifts confirme explicitement un plafond supérieur à 2.
	ConfirmMaxGifts bool
	// NotifyBudget correspond à « Prévenir les participants ».
	NotifyBudget bool
}

// Result décrit le résultat d'une mise à jour.
type Result struct {
	Group         groups.Group
	BudgetChanged bool
}

// UpdateGroupSettings met à jour les réglages du groupe. Le slug ne change
// jamais ; aucune paire n'est lue ni écrite.
func (s *Service) UpdateGroupSettings(ctx context.Context, in Input) (*Result, error) {
	name, err := sanitize.Required(in.Name, groups.NameMaxLen, "Le nom du groupe")
	if err != nil {
		return nil, err
	}
	budget, err := groups.ParseBudget(in.Budget)
	if err != nil {
		return nil, err
	}
	instr, err := sanitize.Text(in.GiftInstructions, groups.GiftInstructionsMax, "La consigne du cadeau")
	if err != nil {
		return nil, err
	}
	ed, err := sanitize.Line(in.EventDate, len(groups.DateLayout), "La date du jour J")
	if err != nil {
		return nil, errs.Validation(msgEventDate)
	}
	if ed != "" {
		if _, err := time.Parse(groups.DateLayout, ed); err != nil {
			return nil, errs.Validation(msgEventDate)
		}
	}
	if m := in.MaxGiftsPerGiver; m != nil {
		if *m < groups.MinMaxGiftsPerGiver || *m > groups.MaxMaxGiftsPerGiver {
			return nil, errs.Validation(msgMaxGiftsOut)
		}
		if *m > groups.DefaultMaxGiftsPerGiver && !in.ConfirmMaxGifts {
			return nil, errs.Validation(msgMaxGiftsConf)
		}
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, errs.Internal(err)
	}
	defer func() { _ = tx.Rollback() }()

	var oldBudget sql.NullInt64
	err = tx.QueryRowContext(ctx, `SELECT budget_cents FROM groups WHERE id = ?`, in.GroupID).Scan(&oldBudget)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, errs.New(errs.KindNotFound, msgNotFound)
	}
	if err != nil {
		return nil, errs.Internal(err)
	}
	changed := oldBudget.Valid != (budget != nil) || (budget != nil && oldBudget.Int64 != *budget)

	var b, date any
	if budget != nil {
		b = *budget
	}
	if ed != "" {
		date = ed
	}
	bump := 0
	if changed {
		bump = 1
	}
	if _, err := tx.ExecContext(ctx,
		`UPDATE groups SET name = ?, budget_cents = ?, budget_version = budget_version + ?, event_date = ?, gift_instructions = ? WHERE id = ?`,
		name, b, bump, date, instr, in.GroupID); err != nil {
		return nil, errs.Internal(err)
	}
	if in.MaxGiftsPerGiver != nil {
		if _, err := tx.ExecContext(ctx, `UPDATE groups SET max_gifts_per_giver = ? WHERE id = ?`, *in.MaxGiftsPerGiver, in.GroupID); err != nil {
			return nil, errs.Internal(err)
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, errs.Internal(err)
	}

	g, err := s.group(ctx, in.GroupID)
	if err != nil {
		return nil, err
	}
	if changed && s.bus != nil {
		ev := ports.Event{Name: EventBudgetChanged, GroupID: in.GroupID, Payload: map[string]string{
			"old_budget": budgetValue(oldBudget.Valid, oldBudget.Int64),
			"new_budget": budgetValue(budget != nil, derefInt(budget)),
			"notify":     strconv.FormatBool(in.NotifyBudget),
			"version":    strconv.Itoa(g.BudgetVersion),
		}}
		if err := s.bus.Publish(ctx, ev); err != nil {
			return nil, errs.Internal(err)
		}
	}
	return &Result{Group: *g, BudgetChanged: changed}, nil
}

func derefInt(p *int64) int64 {
	if p == nil {
		return 0
	}
	return *p
}

// budgetValue écrit un budget en centimes, ou « aucun ».
func budgetValue(has bool, cents int64) string {
	if !has {
		return BudgetNone
	}
	return strconv.FormatInt(cents, 10)
}

func (s *Service) group(ctx context.Context, id int64) (*groups.Group, error) {
	var g groups.Group
	var budget sql.NullInt64
	var date sql.NullString
	err := s.db.QueryRowContext(ctx,
		`SELECT id, slug, name, budget_cents, budget_version, gift_instructions, event_date, max_gifts_per_giver FROM groups WHERE id = ?`, id).
		Scan(&g.ID, &g.Slug, &g.Name, &budget, &g.BudgetVersion, &g.GiftInstructions, &date, &g.MaxGiftsPerGiver)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, errs.New(errs.KindNotFound, msgNotFound)
	}
	if err != nil {
		return nil, errs.Internal(err)
	}
	if budget.Valid {
		v := budget.Int64
		g.BudgetCents = &v
	}
	g.EventDate = date.String
	return &g, nil
}

// SetDefaultLanguage enregistre la langue par défaut du groupe ; une valeur
// vide l'efface (langue de l'instance). Un code non chargé est refusé.
func (s *Service) SetDefaultLanguage(ctx context.Context, groupID int64, lang string) error {
	if lang == "" {
		_, err := s.db.ExecContext(ctx, `DELETE FROM group_settings WHERE group_id = ?`, groupID)
		if err != nil {
			return errs.Internal(err)
		}
		return nil
	}
	if s.langs == nil || !s.langs.Has(lang) {
		return errs.Validation(msgLangUnknown)
	}
	if _, err := s.group(ctx, groupID); err != nil {
		return err
	}
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO group_settings (group_id, default_lang) VALUES (?, ?)
		 ON CONFLICT(group_id) DO UPDATE SET default_lang = excluded.default_lang`, groupID, lang)
	if err != nil {
		return errs.Internal(err)
	}
	return nil
}

// DefaultLanguage relit la langue par défaut du groupe ; ok est faux si elle
// n'est pas définie.
func (s *Service) DefaultLanguage(ctx context.Context, groupID int64) (lang string, ok bool, err error) {
	err = s.db.QueryRowContext(ctx, `SELECT default_lang FROM group_settings WHERE group_id = ?`, groupID).Scan(&lang)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, errs.Internal(err)
	}
	return lang, true, nil
}

// GroupLanguage implémente le port negotiate.GroupLanguage : la langue par
// défaut du groupe désigné par son slug, ok faux si inconnu ou non définie.
func (s *Service) GroupLanguage(ctx context.Context, slug string) (string, bool) {
	var lang string
	err := s.db.QueryRowContext(ctx,
		`SELECT s.default_lang FROM group_settings s JOIN groups g ON g.id = s.group_id WHERE g.slug = ?`, slug).Scan(&lang)
	if err != nil {
		return "", false
	}
	return lang, true
}
