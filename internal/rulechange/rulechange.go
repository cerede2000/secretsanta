// Package rulechange gère la modification des règles (exclusions, foyers,
// forçages) et du rôle dans le tirage après le tirage (F3d.2, F3d.3).
//
// Avant le tirage l'écriture est directe. Après le tirage, le port
// RuleChangeRepair dit si le changement contredit une paire : sinon il est
// enregistré tel quel ; sinon l'administrateur voit un aperçu en comptes
// seulement et choisit la réparation (écriture et réparation dans une seule
// transaction) ou le repli « futurs ajustements seulement ».
//
// Convention du rôle (F3d.3) : ports.ProposedRule ne porte pas le nouveau rôle
// (le port est hors de ce module). Le service envoie donc la nature
// « role » et la seule personne concernée ; une implémentation de réparation
// lit le rôle demandé dans la ligne du participant. Dans Apply, le rappel
// d'écriture est exécuté avant la réparation, la ligne porte alors le nouveau
// rôle. Dans Preview, rien n'est encore écrit : la ligne porte l'ancien rôle,
// et l'aperçu ne peut pas connaître le rôle demandé tant que le port n'est pas
// étendu. Un changement de rôle ne touche jamais aux paires : seule la
// réparation, après confirmation, peut les modifier.
//
// Les textes français de ce module sont des constantes Go (explainPreview,
// explainFuture, explainNoFix, explainRefused et les messages d'erreur) ; ils
// sont à déplacer dans web/static/i18n/rulechange/{fr,en}.json.
package rulechange

import (
	"context"
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"strings"
	"time"

	"secretsanta/internal/db"
	"secretsanta/internal/participants"
	"secretsanta/internal/shared/clock"
	"secretsanta/internal/shared/errs"
	"secretsanta/internal/shared/ports"
)

// Module est le nom du module dans schema_migrations (après groups,
// participants et rules).
const Module = "rulechange"

//go:embed migrations/*.sql
var migrationsFS embed.FS

// Migrations renvoie les migrations embarquées du module.
func Migrations() ([]db.Migration, error) {
	return db.LoadFS(migrationsFS, "migrations")
}

// Choice est la décision de l'administrateur face à un aperçu.
type Choice int

const (
	// ChoiceAsk : ne rien écrire si le changement contredit une paire et
	// renvoyer l'aperçu.
	ChoiceAsk Choice = iota
	// ChoiceRepair : confirmer la réparation du tirage.
	ChoiceRepair
	// ChoiceFutureOnly : refuser la réparation ; le changement est conservé
	// pour les futurs ajustements et nouveaux tirages.
	ChoiceFutureOnly
)

// Status est l'issue d'une demande de changement.
type Status string

const (
	// StatusSaved : enregistré (avant le tirage, sans contradiction, ou
	// réparation appliquée).
	StatusSaved Status = "enregistre"
	// StatusNeedsConfirmation : rien n'est écrit, l'aperçu attend une décision.
	StatusNeedsConfirmation Status = "confirmation"
	// StatusSavedFutureOnly : enregistré avec l'indicateur « n'affecte pas le
	// tirage actuel ».
	StatusSavedFutureOnly Status = "futur_seulement"
)

// Outcome est le résultat d'une demande de changement.
type Outcome struct {
	Status Status
	// Preview ne contient que des comptes (jamais un nom).
	Preview ports.RepairPreview
	// Explanation est le texte français à montrer à l'administrateur.
	Explanation string
	// HouseholdID est l'identifiant du foyer créé (CreateHousehold).
	HouseholdID int64
	// Repaired est vrai si la réparation du tirage a été appliquée.
	Repaired bool
}

// Flag est un changement conservé sans effet sur le tirage actuel.
type Flag struct {
	Kind ports.RuleKind
	A, B int64
}

// Service modifie les règles et les rôles.
type Service struct {
	db     *sql.DB
	clock  clock.Clock
	draw   ports.DrawState
	repair ports.RuleChangeRepair
}

// NewService crée le service.
func NewService(d *sql.DB, c clock.Clock, draw ports.DrawState, repair ports.RuleChangeRepair) *Service {
	return &Service{db: d, clock: c, draw: draw, repair: repair}
}

// execer est satisfait par *sql.DB et *sql.Tx.
type execer interface {
	ExecContext(ctx context.Context, q string, args ...any) (sql.Result, error)
	QueryRowContext(ctx context.Context, q string, args ...any) *sql.Row
}

// RolePreviewer est un port facultatif d'une implémentation de
// ports.RuleChangeRepair : l'aperçu exact d'un changement de rôle, que
// ProposedRule ne peut pas exprimer (elle ne porte pas le rôle demandé). Quand
// la réparation l'offre (repair.Service.PreviewRole), SetRole l'utilise à la
// place de l'aperçu prudent de Preview.
type RolePreviewer interface {
	PreviewRole(ctx context.Context, groupID, participantID int64, role participants.DrawRole) (ports.RepairPreview, error)
}

// change décrit un changement à écrire.
type change struct {
	rule ports.ProposedRule
	// previewRole est le rôle demandé d'un changement de rôle (vide sinon) :
	// il sert à l'aperçu exact quand la réparation est un RolePreviewer.
	previewRole participants.DrawRole
	// flagA et flagB identifient l'indicateur « n'affecte pas le tirage actuel ».
	flagA, flagB int64
	// write écrit le changement ; il renvoie l'identifiant créé éventuel.
	write func(ctx context.Context, ex execer) (int64, error)
	// noPreview : le changement ne peut contredire aucune paire.
	noPreview bool
}

const (
	explainPreview = "Ce changement contredit le tirage actuel : %s. Tu peux demander une réparation avec le moins de changements possible ; les personnes concernées en seront prévenues. Aucun nom n'est affiché ici."
	explainFuture  = "Ce changement est conservé mais il n'affecte pas le tirage actuel : il vaudra pour les futurs ajustements et pour les nouveaux tirages. Aucune paire n'a été modifiée."
	explainNoFix   = "Aucune réparation du tirage actuel n'est possible sans enfreindre une règle. " + explainFuture
	explainRefused = "La réparation a été refusée. " + explainFuture
)

func countsText(p ports.RepairPreview) string {
	plural := func(n int, one, many string) string {
		if n > 1 {
			return many
		}
		return one
	}
	s := fmt.Sprintf("%d %s touchée%s", p.AffectedCount, plural(p.AffectedCount, "personne", "personnes"), plural(p.AffectedCount, "", "s"))
	switch {
	case p.AlreadyDiscoveredCount == 0:
		s += ", dont aucune n'a déjà découvert sa cible"
	case p.AlreadyDiscoveredCount == 1:
		s += ", dont 1 a déjà découvert sa cible"
	default:
		s += fmt.Sprintf(", dont %d ont déjà découvert leur cible", p.AlreadyDiscoveredCount)
	}
	return s
}

func (s *Service) run(ctx context.Context, groupID int64, c change, choice Choice) (Outcome, error) {
	var out Outcome
	var created int64
	direct := func(flag bool) error {
		tx, err := s.db.BeginTx(ctx, nil)
		if err != nil {
			return errs.Internal(err)
		}
		defer func() { _ = tx.Rollback() }()
		id, err := s.writeAll(ctx, tx, groupID, c, flag)
		if err != nil {
			return err
		}
		if err := tx.Commit(); err != nil {
			return errs.Internal(err)
		}
		created = id
		return nil
	}
	finish := func(st Status, expl string) (Outcome, error) {
		out.Status, out.Explanation, out.HouseholdID = st, expl, created
		return out, nil
	}

	done, err := s.draw.IsDrawDone(ctx, groupID)
	if err != nil {
		return out, errs.Internal(err)
	}
	if !done || c.noPreview {
		if err := direct(false); err != nil {
			return out, err
		}
		return finish(StatusSaved, "")
	}
	var prev ports.RepairPreview
	if rp, ok := s.repair.(RolePreviewer); ok && c.previewRole != "" && len(c.rule.Members) == 1 {
		prev, err = rp.PreviewRole(ctx, groupID, c.rule.Members[0], c.previewRole)
	} else {
		prev, err = s.repair.Preview(ctx, groupID, c.rule)
	}
	if err != nil {
		return out, errs.Internal(err)
	}
	out.Preview = prev
	if prev.AffectedCount == 0 {
		if err := direct(false); err != nil {
			return out, err
		}
		return finish(StatusSaved, "")
	}
	if !prev.Repairable {
		if err := direct(true); err != nil {
			return out, err
		}
		return finish(StatusSavedFutureOnly, explainNoFix)
	}
	switch choice {
	case ChoiceFutureOnly:
		if err := direct(true); err != nil {
			return out, err
		}
		return finish(StatusSavedFutureOnly, explainRefused)
	case ChoiceRepair:
		writer := func(ctx context.Context, tx *sql.Tx) error {
			var ex execer = s.db
			if tx != nil {
				ex = tx
			}
			id, err := s.writeAll(ctx, ex, groupID, c, false)
			if err == nil {
				created = id
			}
			return err
		}
		if err := s.repair.Apply(ctx, groupID, c.rule, writer); err != nil {
			var e *errs.Error
			if errors.As(err, &e) {
				return out, err
			}
			return out, errs.Internal(err)
		}
		out.Repaired = true
		return finish(StatusSaved, "")
	default:
		return finish(StatusNeedsConfirmation, fmt.Sprintf(explainPreview, countsText(prev)))
	}
}

// writeAll écrit le changement puis pose ou retire l'indicateur.
func (s *Service) writeAll(ctx context.Context, ex execer, groupID int64, c change, flag bool) (int64, error) {
	id, err := c.write(ctx, ex)
	if err != nil {
		return 0, err
	}
	a, b := c.flagA, c.flagB
	if a == 0 && id != 0 {
		a = id
	}
	if _, err := ex.ExecContext(ctx, `DELETE FROM unaffected_changes WHERE group_id=? AND kind=? AND a_id=? AND b_id=?`,
		groupID, string(c.rule.Kind), a, b); err != nil {
		return 0, errs.Internal(err)
	}
	if flag {
		if _, err := ex.ExecContext(ctx, `INSERT INTO unaffected_changes(group_id, kind, a_id, b_id, created_at) VALUES (?,?,?,?,?)`,
			groupID, string(c.rule.Kind), a, b, s.clock.Now().UTC().Format(time.RFC3339Nano)); err != nil {
			return 0, errs.Internal(err)
		}
	}
	return id, nil
}

// Flags liste les changements conservés sans effet sur le tirage actuel.
func (s *Service) Flags(ctx context.Context, groupID int64) ([]Flag, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT kind, a_id, b_id FROM unaffected_changes WHERE group_id=? ORDER BY id`, groupID)
	if err != nil {
		return nil, errs.Internal(err)
	}
	defer rows.Close()
	var out []Flag
	for rows.Next() {
		var f Flag
		var k string
		if err := rows.Scan(&k, &f.A, &f.B); err != nil {
			return nil, errs.Internal(err)
		}
		f.Kind = ports.RuleKind(k)
		out = append(out, f)
	}
	return out, rows.Err()
}

// ClearFlags efface les indicateurs : à appeler quand un nouveau tirage est
// fait, car les changements valent alors pour le tirage courant.
func (s *Service) ClearFlags(ctx context.Context, groupID int64) error {
	if _, err := s.db.ExecContext(ctx, `DELETE FROM unaffected_changes WHERE group_id=?`, groupID); err != nil {
		return errs.Internal(err)
	}
	return nil
}

// --- validations (lectures seules) ---

func (s *Service) roleOf(ctx context.Context, groupID, id int64) (string, error) {
	var r string
	err := s.db.QueryRowContext(ctx, `SELECT draw_role FROM participants WHERE group_id=? AND id=?`, groupID, id).Scan(&r)
	if errors.Is(err, sql.ErrNoRows) {
		return "", errs.NotFound("Ce participant n'existe pas dans ce groupe.")
	}
	if err != nil {
		return "", errs.Internal(err)
	}
	return r, nil
}

func (s *Service) pairExists(ctx context.Context, table string, groupID, giver, receiver int64) (bool, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM `+table+` WHERE group_id=? AND giver_id=? AND receiver_id=?`,
		groupID, giver, receiver).Scan(&n)
	if err != nil {
		return false, errs.Internal(err)
	}
	return n > 0, nil
}

// sameHousehold dit si deux personnes sont dans le même foyer.
func (s *Service) sameHousehold(ctx context.Context, groupID, a, b int64) (bool, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM household_members x
		JOIN household_members y ON x.household_id = y.household_id
		JOIN households h ON h.id = x.household_id
		WHERE h.group_id=? AND x.participant_id=? AND y.participant_id=?`, groupID, a, b).Scan(&n)
	if err != nil {
		return false, errs.Internal(err)
	}
	return n > 0, nil
}

func (s *Service) forcedAmong(ctx context.Context, groupID int64, members []int64) (bool, error) {
	set := map[int64]bool{}
	for _, m := range members {
		set[m] = true
	}
	rows, err := s.db.QueryContext(ctx, `SELECT giver_id, receiver_id FROM forced_pairs WHERE group_id=?`, groupID)
	if err != nil {
		return false, errs.Internal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var g, r int64
		if err := rows.Scan(&g, &r); err != nil {
			return false, errs.Internal(err)
		}
		if set[g] && set[r] {
			return true, nil
		}
	}
	return false, rows.Err()
}

func (s *Service) householdOwned(ctx context.Context, groupID, householdID int64) error {
	var n int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM households WHERE id=? AND group_id=?`, householdID, groupID).Scan(&n); err != nil {
		return errs.Internal(err)
	}
	if n == 0 {
		return errs.NotFound("Ce foyer n'existe pas dans ce groupe.")
	}
	return nil
}

func (s *Service) members(ctx context.Context, groupID, householdID int64) ([]int64, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT m.participant_id FROM household_members m
		JOIN households h ON h.id = m.household_id WHERE h.id=? AND h.group_id=? ORDER BY m.participant_id`, householdID, groupID)
	if err != nil {
		return nil, errs.Internal(err)
	}
	defer rows.Close()
	var out []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, errs.Internal(err)
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

// --- exclusions et forçages ---

// AddExclusion ajoute « giver n'offre pas à receiver » (un seul sens ; appeler
// deux fois pour la réciproque).
func (s *Service) AddExclusion(ctx context.Context, groupID, giver, receiver int64, choice Choice) (Outcome, error) {
	if giver == receiver {
		return Outcome{}, errs.Validation("Une personne ne peut pas s'offrir un cadeau à elle-même : l'exclusion est inutile.")
	}
	for _, id := range []int64{giver, receiver} {
		if _, err := s.roleOf(ctx, groupID, id); err != nil {
			return Outcome{}, err
		}
	}
	if ok, err := s.pairExists(ctx, "forced_pairs", groupID, giver, receiver); err != nil {
		return Outcome{}, err
	} else if ok {
		return Outcome{}, errs.Conflict("Conflit : cette association est forcée, on ne peut pas aussi l'exclure. Supprimez d'abord l'association forcée.")
	}
	return s.run(ctx, groupID, change{
		rule:  ports.ProposedRule{Kind: ports.RuleExclusion, Members: []int64{giver, receiver}},
		flagA: giver, flagB: receiver,
		write: func(ctx context.Context, ex execer) (int64, error) {
			_, err := ex.ExecContext(ctx, `INSERT OR IGNORE INTO manual_exclusions(group_id, giver_id, receiver_id) VALUES (?,?,?)`, groupID, giver, receiver)
			if err != nil {
				return 0, errs.Internal(err)
			}
			return 0, nil
		},
	}, choice)
}

// AddForced impose « giver offre à receiver ».
func (s *Service) AddForced(ctx context.Context, groupID, giver, receiver int64, choice Choice) (Outcome, error) {
	if giver == receiver {
		return Outcome{}, errs.Validation("Une personne ne peut pas s'offrir un cadeau à elle-même.")
	}
	role, err := s.roleOf(ctx, groupID, giver)
	if err != nil {
		return Outcome{}, err
	}
	if _, err := s.roleOf(ctx, groupID, receiver); err != nil {
		return Outcome{}, err
	}
	if role == string(participants.RoleReceptionSeule) {
		return Outcome{}, errs.Conflict("Association refusée : cette personne est en réception seule, elle reçoit un cadeau mais n'en offre aucun.")
	}
	if ok, err := s.pairExists(ctx, "manual_exclusions", groupID, giver, receiver); err != nil {
		return Outcome{}, err
	} else if ok {
		return Outcome{}, errs.Conflict("Conflit : une exclusion interdit déjà que cette personne offre à l'autre. Supprimez l'exclusion avant de forcer l'association.")
	}
	if ok, err := s.sameHousehold(ctx, groupID, giver, receiver); err != nil {
		return Outcome{}, err
	} else if ok {
		return Outcome{}, errs.Conflict("Conflit : ces deux personnes sont dans le même foyer, qui les exclut l'une de l'autre.")
	}
	return s.run(ctx, groupID, change{
		rule:  ports.ProposedRule{Kind: ports.RuleForcing, Members: []int64{giver, receiver}},
		flagA: giver, flagB: receiver,
		write: func(ctx context.Context, ex execer) (int64, error) {
			_, err := ex.ExecContext(ctx, `INSERT OR IGNORE INTO forced_pairs(group_id, giver_id, receiver_id) VALUES (?,?,?)`, groupID, giver, receiver)
			if err != nil {
				return 0, errs.Internal(err)
			}
			return 0, nil
		},
	}, choice)
}

// RemoveExclusion supprime une exclusion manuelle : lever une contrainte ne
// contredit aucune paire, l'écriture est directe.
func (s *Service) RemoveExclusion(ctx context.Context, groupID, giver, receiver int64) error {
	return s.removePair(ctx, groupID, "manual_exclusions", ports.RuleExclusion, giver, receiver)
}

// RemoveForced supprime un forçage (écriture directe).
func (s *Service) RemoveForced(ctx context.Context, groupID, giver, receiver int64) error {
	return s.removePair(ctx, groupID, "forced_pairs", ports.RuleForcing, giver, receiver)
}

func (s *Service) removePair(ctx context.Context, groupID int64, table string, kind ports.RuleKind, giver, receiver int64) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return errs.Internal(err)
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, `DELETE FROM `+table+` WHERE group_id=? AND giver_id=? AND receiver_id=?`, groupID, giver, receiver); err != nil {
		return errs.Internal(err)
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM unaffected_changes WHERE group_id=? AND kind=? AND a_id=? AND b_id=?`, groupID, string(kind), giver, receiver); err != nil {
		return errs.Internal(err)
	}
	if err := tx.Commit(); err != nil {
		return errs.Internal(err)
	}
	return nil
}

// --- foyers ---

// CreateHousehold crée un foyer ; Outcome.HouseholdID donne son identifiant.
func (s *Service) CreateHousehold(ctx context.Context, groupID int64, name string, members []int64, choice Choice) (Outcome, error) {
	name = strings.TrimSpace(name)
	if n := len([]rune(name)); n < 1 || n > 80 {
		return Outcome{}, errs.Validation("Le nom du foyer doit compter entre 1 et 80 caractères.")
	}
	seen := map[int64]bool{}
	for _, m := range members {
		if seen[m] {
			return Outcome{}, errs.Validation("Un participant est indiqué plusieurs fois dans le foyer.")
		}
		seen[m] = true
		if _, err := s.roleOf(ctx, groupID, m); err != nil {
			return Outcome{}, err
		}
		if err := s.notInHousehold(ctx, m); err != nil {
			return Outcome{}, err
		}
	}
	if err := s.checkForced(ctx, groupID, members); err != nil {
		return Outcome{}, err
	}
	return s.run(ctx, groupID, change{
		rule:      ports.ProposedRule{Kind: ports.RuleHousehold, Members: append([]int64(nil), members...)},
		noPreview: len(members) < 2,
		write: func(ctx context.Context, ex execer) (int64, error) {
			res, err := ex.ExecContext(ctx, `INSERT INTO households(group_id, name) VALUES (?, ?)`, groupID, name)
			if err != nil {
				return 0, errs.Internal(err)
			}
			id, _ := res.LastInsertId()
			for _, m := range members {
				if _, err := ex.ExecContext(ctx, `INSERT INTO household_members(participant_id, household_id) VALUES (?, ?)`, m, id); err != nil {
					return 0, errs.Conflict("Ce participant appartient déjà à un foyer : un participant ne peut appartenir qu'à un seul foyer.")
				}
			}
			return id, nil
		},
	}, choice)
}

// AddHouseholdMember ajoute un participant à un foyer.
func (s *Service) AddHouseholdMember(ctx context.Context, groupID, householdID, participantID int64, choice Choice) (Outcome, error) {
	if err := s.householdOwned(ctx, groupID, householdID); err != nil {
		return Outcome{}, err
	}
	if _, err := s.roleOf(ctx, groupID, participantID); err != nil {
		return Outcome{}, err
	}
	if err := s.notInHousehold(ctx, participantID); err != nil {
		return Outcome{}, err
	}
	cur, err := s.members(ctx, groupID, householdID)
	if err != nil {
		return Outcome{}, err
	}
	all := append(cur, participantID)
	if err := s.checkForced(ctx, groupID, all); err != nil {
		return Outcome{}, err
	}
	return s.run(ctx, groupID, change{
		rule:  ports.ProposedRule{Kind: ports.RuleHousehold, Members: all},
		flagA: householdID, flagB: participantID,
		noPreview: len(cur) == 0,
		write: func(ctx context.Context, ex execer) (int64, error) {
			if _, err := ex.ExecContext(ctx, `INSERT INTO household_members(participant_id, household_id) VALUES (?, ?)`, participantID, householdID); err != nil {
				return 0, errs.Conflict("Ce participant appartient déjà à un foyer : un participant ne peut appartenir qu'à un seul foyer.")
			}
			return 0, nil
		},
	}, choice)
}

// RemoveHouseholdMember retire un participant d'un foyer (écriture directe :
// lever une exclusion ne contredit aucune paire).
func (s *Service) RemoveHouseholdMember(ctx context.Context, groupID, householdID, participantID int64) error {
	if err := s.householdOwned(ctx, groupID, householdID); err != nil {
		return err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return errs.Internal(err)
	}
	defer func() { _ = tx.Rollback() }()
	res, err := tx.ExecContext(ctx, `DELETE FROM household_members WHERE household_id=? AND participant_id=?`, householdID, participantID)
	if err != nil {
		return errs.Internal(err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return errs.NotFound("Ce participant n'est pas membre de ce foyer.")
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM unaffected_changes WHERE group_id=? AND kind='foyer' AND a_id=? AND b_id=?`, groupID, householdID, participantID); err != nil {
		return errs.Internal(err)
	}
	if err := tx.Commit(); err != nil {
		return errs.Internal(err)
	}
	return nil
}

func (s *Service) notInHousehold(ctx context.Context, participantID int64) error {
	var n int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM household_members WHERE participant_id=?`, participantID).Scan(&n); err != nil {
		return errs.Internal(err)
	}
	if n > 0 {
		return errs.Conflict("Ce participant appartient déjà à un foyer : un participant ne peut appartenir qu'à un seul foyer.")
	}
	return nil
}

func (s *Service) checkForced(ctx context.Context, groupID int64, members []int64) error {
	ok, err := s.forcedAmong(ctx, groupID, members)
	if err != nil {
		return err
	}
	if ok {
		return errs.Conflict("Conflit : un foyer exclut ses membres entre eux, or une association forcée fait offrir l'un à l'autre. Supprimez l'association forcée d'abord.")
	}
	return nil
}

// --- rôle ---

// SetRole change le rôle dans le tirage d'une personne (action de
// l'administrateur : l'appelant a déjà contrôlé la session). Avant le tirage
// l'écriture est directe ; après, le port RuleChangeRepair décide. Jamais de
// nouveau tirage silencieux. Note : ProposedRule ne porte pas le nouveau rôle ;
// la réparation le lit dans le rôle écrit par le rappel d'écriture, et l'aperçu
// passe par RolePreviewer quand la réparation l'offre.
func (s *Service) SetRole(ctx context.Context, groupID, participantID int64, role participants.DrawRole, choice Choice) (Outcome, error) {
	if !role.Valid() {
		return Outcome{}, errs.Validation("Ce rôle n'existe pas : choisissez normal, réception seule, offre plus ou un seul.")
	}
	cur, err := s.roleOf(ctx, groupID, participantID)
	if err != nil {
		return Outcome{}, err
	}
	if cur == string(role) {
		return Outcome{Status: StatusSaved}, nil
	}
	return s.run(ctx, groupID, change{
		rule:        ports.ProposedRule{Kind: ports.RuleRoleChange, Members: []int64{participantID}},
		previewRole: role,
		flagA:       participantID,
		write: func(ctx context.Context, ex execer) (int64, error) {
			if _, err := ex.ExecContext(ctx, `UPDATE participants SET draw_role=? WHERE group_id=? AND id=?`, string(role), groupID, participantID); err != nil {
				return 0, errs.Internal(err)
			}
			return 0, nil
		},
	}, choice)
}
