// Package rules porte les contraintes du tirage (F3.2) : foyers, exclusions
// manuelles et associations forcées, avec détection des conflits expliquée
// en français et la fonction BuildConstraints pour F4.
package rules

import (
	"context"
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"sort"
	"strings"

	"secretsanta/internal/db"
	"secretsanta/internal/shared/errs"
)

// Module est le nom du module dans schema_migrations.
const Module = "rules"

//go:embed migrations/*.sql
var migrationsFS embed.FS

// Migrations renvoie les migrations embarquées du module.
func Migrations() ([]db.Migration, error) {
	return db.LoadFS(migrationsFS, "migrations")
}

// RoleReceptionSeule est la valeur de draw_role d'une personne qui ne fait
// que recevoir.
const RoleReceptionSeule = "reception_seule"

// Directory est le port de lecture des participants (fourni par
// internal/participants, défini ici pour ne pas en dépendre).
type Directory interface {
	// DrawRole renvoie le rôle du participant dans le groupe ; ok est faux
	// si le participant n'existe pas dans ce groupe.
	DrawRole(ctx context.Context, groupID, participantID int64) (role string, ok bool, err error)
}

// Pair est un couple tireur / bénéficiaire.
type Pair struct {
	Giver    int64
	Receiver int64
}

// Constraints est le jeu de contraintes remis à F4.
type Constraints struct {
	// Exclusions : couples interdits (foyers et manuelles, dédoublonnés,
	// triés).
	Exclusions []Pair
	// Forced : associations imposées.
	Forced []Pair
	// Ineffective : exclusions dont le tireur est en reception_seule ; elles
	// n'ont aucun effet.
	Ineffective []Pair
}

// Service gère les règles.
type Service struct {
	db  *sql.DB
	dir Directory
}

// NewService crée le service.
func NewService(d *sql.DB, dir Directory) *Service { return &Service{db: d, dir: dir} }

func (s *Service) checkMember(ctx context.Context, groupID, id int64) (string, error) {
	role, ok, err := s.dir.DrawRole(ctx, groupID, id)
	if err != nil {
		return "", errs.Internal(err)
	}
	if !ok {
		return "", errs.NotFound("Ce participant n'existe pas dans ce groupe.")
	}
	return role, nil
}

func (s *Service) householdOwned(ctx context.Context, groupID, householdID int64) error {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM households WHERE id=? AND group_id=?`, householdID, groupID).Scan(&n)
	if err != nil {
		return errs.Internal(err)
	}
	if n == 0 {
		return errs.NotFound("Ce foyer n'existe pas dans ce groupe.")
	}
	return nil
}

// CreateHousehold crée un foyer avec ses membres.
func (s *Service) CreateHousehold(ctx context.Context, groupID int64, name string, members []int64) (int64, error) {
	name = strings.TrimSpace(name)
	if n := len([]rune(name)); n < 1 || n > 80 {
		return 0, errs.Validation("Le nom du foyer doit compter entre 1 et 80 caractères.")
	}
	for _, m := range members {
		if _, err := s.checkMember(ctx, groupID, m); err != nil {
			return 0, err
		}
	}
	if len(uniq(members)) != len(members) {
		return 0, errs.Validation("Un participant est indiqué plusieurs fois dans le foyer.")
	}
	if err := s.checkHouseholdVsForced(ctx, groupID, members); err != nil {
		return 0, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, errs.Internal(err)
	}
	defer func() { _ = tx.Rollback() }()
	res, err := tx.ExecContext(ctx, `INSERT INTO households(group_id, name) VALUES (?, ?)`, groupID, name)
	if err != nil {
		return 0, errs.Internal(err)
	}
	id, _ := res.LastInsertId()
	for _, m := range members {
		if err := insertMember(ctx, tx, id, m); err != nil {
			return 0, err
		}
	}
	if err := tx.Commit(); err != nil {
		return 0, errs.Internal(err)
	}
	return id, nil
}

func insertMember(ctx context.Context, tx *sql.Tx, householdID, participantID int64) error {
	var existing int64
	err := tx.QueryRowContext(ctx, `SELECT household_id FROM household_members WHERE participant_id=?`, participantID).Scan(&existing)
	if err == nil {
		return errs.Conflict("Ce participant appartient déjà à un foyer : un participant ne peut appartenir qu'à un seul foyer.")
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return errs.Internal(err)
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO household_members(participant_id, household_id) VALUES (?, ?)`, participantID, householdID); err != nil {
		return errs.Conflict("Ce participant appartient déjà à un foyer : un participant ne peut appartenir qu'à un seul foyer.")
	}
	return nil
}

// AddMember ajoute un participant à un foyer.
func (s *Service) AddMember(ctx context.Context, groupID, householdID, participantID int64) error {
	if err := s.householdOwned(ctx, groupID, householdID); err != nil {
		return err
	}
	if _, err := s.checkMember(ctx, groupID, participantID); err != nil {
		return err
	}
	current, err := s.Members(ctx, groupID, householdID)
	if err != nil {
		return err
	}
	if err := s.checkHouseholdVsForced(ctx, groupID, append(current, participantID)); err != nil {
		return err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return errs.Internal(err)
	}
	defer func() { _ = tx.Rollback() }()
	if err := insertMember(ctx, tx, householdID, participantID); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return errs.Internal(err)
	}
	return nil
}

// RemoveMember retire un participant de son foyer ; les exclusions que le
// foyer générait pour lui disparaissent.
func (s *Service) RemoveMember(ctx context.Context, groupID, householdID, participantID int64) error {
	if err := s.householdOwned(ctx, groupID, householdID); err != nil {
		return err
	}
	res, err := s.db.ExecContext(ctx, `DELETE FROM household_members WHERE household_id=? AND participant_id=?`, householdID, participantID)
	if err != nil {
		return errs.Internal(err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return errs.NotFound("Ce participant n'est pas membre de ce foyer.")
	}
	return nil
}

// DeleteHousehold supprime un foyer ; confirmed doit être vrai.
func (s *Service) DeleteHousehold(ctx context.Context, groupID, householdID int64, confirmed bool) error {
	if !confirmed {
		return errs.Validation("La suppression d'un foyer doit être confirmée.")
	}
	res, err := s.db.ExecContext(ctx, `DELETE FROM households WHERE id=? AND group_id=?`, householdID, groupID)
	if err != nil {
		return errs.Internal(err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return errs.NotFound("Ce foyer n'existe pas dans ce groupe.")
	}
	return nil
}

// Members liste les membres d'un foyer, triés.
func (s *Service) Members(ctx context.Context, groupID, householdID int64) ([]int64, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT m.participant_id FROM household_members m
		JOIN households h ON h.id = m.household_id
		WHERE h.id=? AND h.group_id=? ORDER BY m.participant_id`, householdID, groupID)
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

// ForgetParticipant retire un participant supprimé de tous les foyers,
// exclusions et forçages de son groupe.
func (s *Service) ForgetParticipant(ctx context.Context, groupID, participantID int64) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return errs.Internal(err)
	}
	defer func() { _ = tx.Rollback() }()
	for _, q := range []string{
		`DELETE FROM household_members WHERE participant_id=? AND household_id IN (SELECT id FROM households WHERE group_id=?)`,
		`DELETE FROM manual_exclusions WHERE (giver_id=? OR receiver_id=?) AND group_id=?`,
		`DELETE FROM forced_pairs WHERE (giver_id=? OR receiver_id=?) AND group_id=?`,
	} {
		var args []any
		switch strings.Count(q, "?") {
		case 2:
			args = []any{participantID, groupID}
		default:
			args = []any{participantID, participantID, groupID}
		}
		if _, err := tx.ExecContext(ctx, q, args...); err != nil {
			return errs.Internal(err)
		}
	}
	return tx.Commit()
}

// ExclusionResult décrit le résultat d'une saisie d'exclusion.
type ExclusionResult struct {
	// Warnings : explications en français (par exemple exclusion sans effet).
	Warnings []string
}

// AddExclusion ajoute une exclusion « giver n'offre pas à receiver », et la
// réciproque si bothWays. Elle est refusée si elle contredit un forçage.
func (s *Service) AddExclusion(ctx context.Context, groupID, giver, receiver int64, bothWays bool) (ExclusionResult, error) {
	var res ExclusionResult
	if giver == receiver {
		return res, errs.Validation("Une personne ne peut pas s'offrir un cadeau à elle-même : l'exclusion est inutile.")
	}
	pairs := []Pair{{giver, receiver}}
	if bothWays {
		pairs = append(pairs, Pair{receiver, giver})
	}
	roles := map[int64]string{}
	for _, id := range []int64{giver, receiver} {
		r, err := s.checkMember(ctx, groupID, id)
		if err != nil {
			return res, err
		}
		roles[id] = r
	}
	forced, err := s.forced(ctx, groupID)
	if err != nil {
		return res, err
	}
	for _, p := range pairs {
		for _, f := range forced {
			if f == p {
				return res, errs.Conflict(fmt.Sprintf(
					"Conflit : le participant %d doit offrir à %d (association forcée), on ne peut pas aussi l'exclure. Supprimez d'abord l'association forcée.",
					p.Giver, p.Receiver))
			}
		}
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return res, errs.Internal(err)
	}
	defer func() { _ = tx.Rollback() }()
	for _, p := range pairs {
		if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO manual_exclusions(group_id, giver_id, receiver_id) VALUES (?,?,?)`, groupID, p.Giver, p.Receiver); err != nil {
			return res, errs.Internal(err)
		}
		if roles[p.Giver] == RoleReceptionSeule {
			res.Warnings = append(res.Warnings, fmt.Sprintf(
				"Cette exclusion est sans effet : le participant %d est en réception seule et n'offre aucun cadeau.", p.Giver))
		}
	}
	if err := tx.Commit(); err != nil {
		return res, errs.Internal(err)
	}
	return res, nil
}

// RemoveExclusion supprime une exclusion manuelle (et sa réciproque si bothWays).
func (s *Service) RemoveExclusion(ctx context.Context, groupID, giver, receiver int64, bothWays bool) error {
	q := `DELETE FROM manual_exclusions WHERE group_id=? AND giver_id=? AND receiver_id=?`
	if _, err := s.db.ExecContext(ctx, q, groupID, giver, receiver); err != nil {
		return errs.Internal(err)
	}
	if bothWays {
		if _, err := s.db.ExecContext(ctx, q, groupID, receiver, giver); err != nil {
			return errs.Internal(err)
		}
	}
	return nil
}

// AddForced impose « giver offre à receiver ». Refusé si le tireur est en
// réception seule ou si une exclusion (manuelle ou de foyer) l'interdit.
func (s *Service) AddForced(ctx context.Context, groupID, giver, receiver int64) error {
	if giver == receiver {
		return errs.Validation("Une personne ne peut pas s'offrir un cadeau à elle-même.")
	}
	role, err := s.checkMember(ctx, groupID, giver)
	if err != nil {
		return err
	}
	if _, err := s.checkMember(ctx, groupID, receiver); err != nil {
		return err
	}
	if role == RoleReceptionSeule {
		return errs.Conflict("Association refusée : cette personne est en réception seule, elle reçoit un cadeau mais n'en offre aucun.")
	}
	excl, err := s.exclusions(ctx, groupID)
	if err != nil {
		return err
	}
	for _, e := range excl {
		if e == (Pair{giver, receiver}) {
			return errs.Conflict("Conflit : une exclusion (manuelle ou issue d'un foyer) interdit déjà que cette personne offre à l'autre. Supprimez l'exclusion avant de forcer l'association.")
		}
	}
	if _, err := s.db.ExecContext(ctx, `INSERT OR IGNORE INTO forced_pairs(group_id, giver_id, receiver_id) VALUES (?,?,?)`, groupID, giver, receiver); err != nil {
		return errs.Internal(err)
	}
	return nil
}

// RemoveForced supprime un forçage.
func (s *Service) RemoveForced(ctx context.Context, groupID, giver, receiver int64) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM forced_pairs WHERE group_id=? AND giver_id=? AND receiver_id=?`, groupID, giver, receiver)
	if err != nil {
		return errs.Internal(err)
	}
	return nil
}

func (s *Service) checkHouseholdVsForced(ctx context.Context, groupID int64, members []int64) error {
	forced, err := s.forced(ctx, groupID)
	if err != nil {
		return err
	}
	set := map[int64]bool{}
	for _, m := range members {
		set[m] = true
	}
	for _, f := range forced {
		if set[f.Giver] && set[f.Receiver] {
			return errs.Conflict("Conflit : un foyer exclut ses membres entre eux, or une association forcée fait offrir l'un à l'autre. Supprimez l'association forcée d'abord.")
		}
	}
	return nil
}

func (s *Service) forced(ctx context.Context, groupID int64) ([]Pair, error) {
	return s.pairs(ctx, `SELECT giver_id, receiver_id FROM forced_pairs WHERE group_id=?`, groupID)
}

func (s *Service) pairs(ctx context.Context, q string, args ...any) ([]Pair, error) {
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, errs.Internal(err)
	}
	defer rows.Close()
	var out []Pair
	for rows.Next() {
		var p Pair
		if err := rows.Scan(&p.Giver, &p.Receiver); err != nil {
			return nil, errs.Internal(err)
		}
		out = append(out, p)
	}
	if err := rows.Err(); err != nil {
		return nil, errs.Internal(err)
	}
	return out, nil
}

// exclusions renvoie les exclusions de foyers et manuelles, dédoublonnées et triées.
func (s *Service) exclusions(ctx context.Context, groupID int64) ([]Pair, error) {
	manual, err := s.pairs(ctx, `SELECT giver_id, receiver_id FROM manual_exclusions WHERE group_id=?`, groupID)
	if err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx, `SELECT m.household_id, m.participant_id FROM household_members m
		JOIN households h ON h.id = m.household_id WHERE h.group_id=? ORDER BY m.household_id, m.participant_id`, groupID)
	if err != nil {
		return nil, errs.Internal(err)
	}
	defer rows.Close()
	byHouse := map[int64][]int64{}
	for rows.Next() {
		var h, p int64
		if err := rows.Scan(&h, &p); err != nil {
			return nil, errs.Internal(err)
		}
		byHouse[h] = append(byHouse[h], p)
	}
	if err := rows.Err(); err != nil {
		return nil, errs.Internal(err)
	}
	seen := map[Pair]bool{}
	var out []Pair
	add := func(p Pair) {
		if !seen[p] {
			seen[p] = true
			out = append(out, p)
		}
	}
	for _, p := range manual {
		add(p)
	}
	for _, ms := range byHouse {
		for _, a := range ms {
			for _, b := range ms {
				if a != b {
					add(Pair{a, b})
				}
			}
		}
	}
	sortPairs(out)
	return out, nil
}

// BuildConstraints renvoie les exclusions (foyers et manuelles) et les
// forçages du groupe, pour F4.
func (s *Service) BuildConstraints(ctx context.Context, groupID int64) (Constraints, error) {
	var c Constraints
	excl, err := s.exclusions(ctx, groupID)
	if err != nil {
		return c, err
	}
	c.Exclusions = excl
	if c.Forced, err = s.forced(ctx, groupID); err != nil {
		return c, err
	}
	sortPairs(c.Forced)
	roles := map[int64]string{}
	for _, e := range excl {
		r, ok := roles[e.Giver]
		if !ok {
			role, found, err := s.dir.DrawRole(ctx, groupID, e.Giver)
			if err != nil {
				return c, errs.Internal(err)
			}
			if found {
				r = role
			}
			roles[e.Giver] = r
		}
		if r == RoleReceptionSeule {
			c.Ineffective = append(c.Ineffective, e)
		}
	}
	return c, nil
}

func sortPairs(p []Pair) {
	sort.Slice(p, func(i, j int) bool {
		if p[i].Giver != p[j].Giver {
			return p[i].Giver < p[j].Giver
		}
		return p[i].Receiver < p[j].Receiver
	})
}

func uniq(in []int64) map[int64]bool {
	m := map[int64]bool{}
	for _, v := range in {
		m[v] = true
	}
	return m
}
