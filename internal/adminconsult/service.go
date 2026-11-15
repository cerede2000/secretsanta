// Package adminconsult porte la consultation de paire optionnelle (F10b.1).
//
// L'option de groupe « l'administrateur peut consulter une paire »
// (groups.admin_can_view_pair, posée à la création, désactivée par défaut)
// ouvre une consultation PAR PERSONNE : l'administrateur ressaisit son mot de
// passe d'administration, choisit une personne et voit la ou les cibles de
// cette seule personne. Aucune méthode ne renvoie le tableau complet des
// paires. Chaque consultation est consignée, dans la même transaction que la
// lecture, dans un journal visible de tous les participants : une date, sans
// nom, sans cible ; aucun courriel n'est envoyé.
//
// Quand l'option est désactivée, la consultation est refusée par le service
// et les routes de consultation répondent comme une route inexistante.
package adminconsult

import (
	"context"
	"database/sql"
	"embed"
	"errors"
	"time"

	"secretsanta/internal/access/limiter"
	"secretsanta/internal/db"
	"secretsanta/internal/groups"
	"secretsanta/internal/participants"
	"secretsanta/internal/shared/clock"
	"secretsanta/internal/shared/errs"
)

// Module est le nom du module dans schema_migrations.
const Module = "adminconsult"

//go:embed migrations/*.sql
var migrationsFS embed.FS

// Migrations renvoie les migrations embarquées, à enregistrer par
// l'orchestrateur après celles de groups (clé étrangère) ; le service lit
// aussi les tables du tirage (draws, draw_pairs) et des participants.
func Migrations() ([]db.Migration, error) { return db.LoadFS(migrationsFS, "migrations") }

// Codes d'erreur (errs.Error.WithCode).
const (
	// CodeDisabled : l'option de consultation est désactivée pour ce groupe.
	CodeDisabled = "consultation_desactivee"
	// CodeBadPassword : le mot de passe d'administration ressaisi est faux.
	CodeBadPassword = "mot_de_passe_incorrect"
	// CodeNoDraw : aucun tirage actuel, il n'y a rien à consulter.
	CodeNoDraw = "aucun_tirage"
)

// PasswordAttempts borne les échecs de ressaisie du mot de passe : 5 par
// groupe sur 15 minutes.
var PasswordAttempts = limiter.Rule{Limit: 5, Window: 15 * time.Minute}

// MaxJournal est le nombre maximal de lignes renvoyées par Journal.
const MaxJournal = 200

// Messages français du service (les pages passent par le catalogue).
const (
	msgDisabled    = "La consultation de paire n'est pas activée pour ce groupe."
	msgForbidden   = "Tu n'as pas le droit d'effectuer cette action."
	msgAuth        = "Connecte-toi pour continuer."
	msgBadPassword = "Le mot de passe d'administration est incorrect."
	msgNoDraw      = "Le tirage n'a pas encore eu lieu."
	msgNotFound    = "Cette personne n'existe pas dans le groupe."
)

// Groups est la part du service des groupes utilisée ici.
type Groups interface {
	ByID(ctx context.Context, id int64) (*groups.Group, error)
	VerifyAdminSecret(ctx context.Context, slug, secret string) (int64, error)
}

// People est la part du service des participants utilisée ici.
type People interface {
	Get(ctx context.Context, a participants.Actor, groupID, id int64) (*participants.Participant, error)
	List(ctx context.Context, a participants.Actor, groupID int64) ([]participants.Participant, error)
}

// Person est une personne affichée par la consultation : prénom et nom.
type Person struct {
	ID        int64
	FirstName string
	Name      string
}

// Consultation est le résultat d'une consultation : UNE personne et sa ou ses
// cibles dans le tirage actuel (aucune pour une personne qui ne fait que
// recevoir).
type Consultation struct {
	Giver   Person
	Targets []Person
	At      time.Time
}

// Entry est une ligne du journal des consultations : une date, rien d'autre.
type Entry struct {
	At time.Time
}

// Service tient la consultation et son journal.
type Service struct {
	db     *sql.DB
	clk    clock.Clock
	groups Groups
	people People
	lim    *limiter.Limiter
}

// NewService crée le service. Il ne dépend d'aucun envoi de courriel : une
// consultation ne prévient personne par courriel.
func NewService(d *sql.DB, c clock.Clock, g Groups, p People) (*Service, error) {
	if d == nil || c == nil || g == nil || p == nil {
		return nil, errors.New("adminconsult: dépendance manquante")
	}
	return &Service{db: d, clk: c, groups: g, people: p, lim: limiter.New(c, PasswordAttempts)}, nil
}

func requireAdmin(a participants.Actor, groupID int64) error {
	if a.Kind == participants.ActorAnonymous {
		return errs.New(errs.KindUnauthorized, msgAuth)
	}
	if a.Kind != participants.ActorAdmin || a.GroupID != groupID || groupID == 0 {
		return errs.Forbidden(msgForbidden)
	}
	return nil
}

func requireMember(a participants.Actor, groupID int64) error {
	if a.Kind == participants.ActorAnonymous {
		return errs.New(errs.KindUnauthorized, msgAuth)
	}
	if a.GroupID != groupID || groupID == 0 {
		return errs.Forbidden(msgForbidden)
	}
	if a.Kind == participants.ActorParticipant && a.ParticipantID == 0 {
		return errs.Forbidden(msgForbidden)
	}
	return nil
}

func errDisabled() error { return errs.NotFound(msgDisabled).WithCode(CodeDisabled) }

// Enabled dit si l'option de consultation est active pour le groupe. Un
// groupe inconnu n'a pas l'option.
func (s *Service) Enabled(ctx context.Context, groupID int64) (bool, error) {
	g, err := s.groups.ByID(ctx, groupID)
	if err != nil {
		if errs.Is(err, errs.KindNotFound) {
			return false, nil
		}
		return false, err
	}
	return g != nil && g.AdminCanViewPair, nil
}

// People renvoie les personnes que l'administrateur peut choisir : prénom et
// nom seulement, jamais de paire. Refusé quand l'option est désactivée.
func (s *Service) People(ctx context.Context, a participants.Actor, groupID int64) ([]Person, error) {
	if err := requireAdmin(a, groupID); err != nil {
		return nil, err
	}
	on, err := s.Enabled(ctx, groupID)
	if err != nil {
		return nil, err
	}
	if !on {
		return nil, errDisabled()
	}
	list, err := s.people.List(ctx, a, groupID)
	if err != nil {
		return nil, err
	}
	out := make([]Person, 0, len(list))
	for _, p := range list {
		out = append(out, Person{ID: p.ID, FirstName: p.FirstName, Name: p.Name})
	}
	return out, nil
}

func passwordKey(groupID int64) limiter.Key { return limiter.TargetKey(groupID, "consultation_paire") }

// Consult révèle à l'administrateur la ou les cibles d'UNE personne du
// tirage actuel, après ressaisie du mot de passe d'administration. Refus :
// option désactivée (CodeDisabled), trop d'échecs (limiter.ErrLimited), mot
// de passe faux (CodeBadPassword), personne inconnue, aucun tirage
// (CodeNoDraw). Une consultation réussie est consignée au journal dans la
// même transaction que la lecture des paires.
func (s *Service) Consult(ctx context.Context, a participants.Actor, groupID, personID int64, password string) (*Consultation, error) {
	if err := requireAdmin(a, groupID); err != nil {
		return nil, err
	}
	g, err := s.groups.ByID(ctx, groupID)
	if err != nil {
		if errs.Is(err, errs.KindNotFound) {
			return nil, errDisabled()
		}
		return nil, err
	}
	if g == nil || !g.AdminCanViewPair {
		return nil, errDisabled()
	}
	key := passwordKey(groupID)
	if d := s.lim.Peek(key); !d.Allowed {
		return nil, limiter.Error()
	}
	id, verr := s.groups.VerifyAdminSecret(ctx, g.Slug, password)
	if verr != nil || id != groupID {
		if verr != nil && errs.KindOf(verr) == errs.KindInternal {
			return nil, verr
		}
		s.lim.Hit(key)
		return nil, errs.New(errs.KindUnauthorized, msgBadPassword).WithCode(CodeBadPassword)
	}
	admin := participants.AdminActor(groupID)
	giver, err := s.people.Get(ctx, admin, groupID, personID)
	if err != nil || giver == nil || giver.GroupID != groupID {
		if err != nil && errs.KindOf(err) == errs.KindInternal {
			return nil, err
		}
		return nil, errs.NotFound(msgNotFound)
	}
	receivers, at, err := s.readAndRecord(ctx, groupID, personID)
	if err != nil {
		return nil, err
	}
	out := &Consultation{Giver: Person{ID: giver.ID, FirstName: giver.FirstName, Name: giver.Name}, At: at}
	for _, rid := range receivers {
		p, err := s.people.Get(ctx, admin, groupID, rid)
		if err != nil || p == nil {
			// Une cible supprimée depuis le tirage : on ne montre que son
			// absence, jamais un autre nom.
			out.Targets = append(out.Targets, Person{ID: rid})
			continue
		}
		out.Targets = append(out.Targets, Person{ID: p.ID, FirstName: p.FirstName, Name: p.Name})
	}
	return out, nil
}

// readAndRecord lit les cibles de la personne dans le tirage actuel et
// consigne la consultation, dans une seule transaction.
func (s *Service) readAndRecord(ctx context.Context, groupID, giverID int64) ([]int64, time.Time, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, time.Time{}, errs.Internal(err)
	}
	defer func() { _ = tx.Rollback() }()
	var drawID int64
	err = tx.QueryRowContext(ctx, `SELECT id FROM draws WHERE group_id = ? AND status = 'actuel'`, groupID).Scan(&drawID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, time.Time{}, errs.Conflict(msgNoDraw).WithCode(CodeNoDraw)
	}
	if err != nil {
		return nil, time.Time{}, errs.Internal(err)
	}
	rows, err := tx.QueryContext(ctx, `SELECT receiver_id FROM draw_pairs WHERE draw_id = ? AND giver_id = ? ORDER BY id`, drawID, giverID)
	if err != nil {
		return nil, time.Time{}, errs.Internal(err)
	}
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			_ = rows.Close()
			return nil, time.Time{}, errs.Internal(err)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return nil, time.Time{}, errs.Internal(err)
	}
	_ = rows.Close()
	at := s.clk.Now().UTC()
	if _, err := tx.ExecContext(ctx, `INSERT INTO adminconsult_journal (group_id, at) VALUES (?, ?)`, groupID, at.UnixNano()); err != nil {
		return nil, time.Time{}, errs.Internal(err)
	}
	if err := tx.Commit(); err != nil {
		return nil, time.Time{}, errs.Internal(err)
	}
	return ids, at, nil
}

// Journal renvoie les consultations du groupe, de la plus récente à la plus
// ancienne (au plus MaxJournal). Tout membre du groupe peut le lire :
// l'administrateur et chaque participant connecté.
func (s *Service) Journal(ctx context.Context, a participants.Actor, groupID int64) ([]Entry, error) {
	if err := requireMember(a, groupID); err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx, `SELECT at FROM adminconsult_journal WHERE group_id = ? ORDER BY id DESC LIMIT ?`,
		groupID, MaxJournal)
	if err != nil {
		return nil, errs.Internal(err)
	}
	defer rows.Close()
	var out []Entry
	for rows.Next() {
		var at int64
		if err := rows.Scan(&at); err != nil {
			return nil, errs.Internal(err)
		}
		out = append(out, Entry{At: time.Unix(0, at).UTC()})
	}
	if err := rows.Err(); err != nil {
		return nil, errs.Internal(err)
	}
	return out, nil
}
