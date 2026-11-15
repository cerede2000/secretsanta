package draw

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"secretsanta/internal/drawengine"
	"secretsanta/internal/participants"
	"secretsanta/internal/shared/clock"
	"secretsanta/internal/shared/errs"
	"secretsanta/internal/shared/ports"
)

// Messages français.
const (
	msgAuth           = "Authentification requise."
	msgForbidden      = "Tu n'as pas le droit d'effectuer cette action."
	msgConfirmDraw    = "Le tirage doit être confirmé avant d'être lancé."
	msgConfirmRedraw  = "Le nouveau tirage doit être confirmé : l'ancien tirage sera archivé et toutes les cibles changeront."
	msgAlreadyDone    = "Le tirage est déjà fait. Pour tout recommencer, lancez un nouveau tirage."
	msgNotDone        = "Le tirage n'est pas encore fait."
	msgChanged        = "Le tirage a changé entre-temps. Recharge la page et recommence."
	msgRosterChanged  = "La liste des participants a changé pendant le tirage. Relancez-le."
	msgResolution     = "Choisis d'ignorer ou de supprimer les inscriptions en attente."
	msgNotYourTarget  = "Cette cible n'existe pas."
	msgNotOpen        = "La cible n'est pas encore révélée."
	msgAdjustInvalid  = "L'ajustement est incohérent : chaque personne doit recevoir exactement un cadeau et personne ne peut s'offrir son propre cadeau."
	msgNoRegistration = "Les inscriptions en attente ne peuvent pas être vérifiées pour le moment."
)

// Codes d'erreur (pour les catalogues de textes de l'orchestrateur).
const (
	CodePendingRegistrations = "draw.pending_registrations"
	CodeInfeasible           = "draw.infeasible"
)

// Options règle le service.
type Options struct {
	// Location fixe l'année d'un tirage (fuseau APP_TIMEZONE) ; nil vaut UTC.
	Location *time.Location
	// Gate remplace la porte de révélation par défaut (date du groupe).
	Gate RevealGate
	// Engine règle le moteur ; nil vaut drawengine.DefaultOptions().
	Engine *drawengine.Options
}

// Service exécute et expose le tirage.
type Service struct {
	*State
	db            *sql.DB
	clock         clock.Clock
	groups        GroupSource
	roster        RosterSource
	constraints   ConstraintSource
	registrations ports.RegistrationState
	gate          RevealGate
	loc           *time.Location
	engine        drawengine.Options
}

// NewService construit le service du tirage.
func NewService(d *sql.DB, c clock.Clock, g GroupSource, r RosterSource, cs ConstraintSource,
	reg ports.RegistrationState, opts Options) *Service {
	s := &Service{
		State: NewState(d), db: d, clock: c, groups: g, roster: r, constraints: cs,
		registrations: reg, gate: opts.Gate, loc: opts.Location, engine: drawengine.DefaultOptions(),
	}
	if s.loc == nil {
		s.loc = time.UTC
	}
	if opts.Engine != nil {
		s.engine = *opts.Engine
	}
	if s.gate == nil {
		s.gate = DateGate{Groups: g, Clock: c}
	}
	return s
}

// DateGate est la porte par défaut : ouverte sans date de révélation, ou
// quand l'horloge a atteint la date.
type DateGate struct {
	Groups GroupSource
	Clock  clock.Clock
}

// IsOpen applique la date de révélation du groupe.
func (g DateGate) IsOpen(ctx context.Context, groupID int64) (bool, error) {
	gr, err := g.Groups.ByID(ctx, groupID)
	if err != nil {
		return false, err
	}
	if gr.RevealAt == nil {
		return true, nil
	}
	return !g.Clock.Now().Before(*gr.RevealAt), nil
}

// Request est la demande de tirage.
type Request struct {
	// Confirmed est l'indicateur de confirmation, exigé.
	Confirmed bool
	// CapConfirmed confirme un plafond de plus de 2 cadeaux par tireur.
	CapConfirmed bool
}

// Outcome est le résultat d'un tirage : aucune paire, seulement des faits
// et des décomptes.
type Outcome struct {
	DrawID int64
	Year   int
	Report CheckReport
}

func requireAdmin(a participants.Actor, groupID int64) error {
	switch {
	case a.Kind == participants.ActorAnonymous:
		return errs.New(errs.KindUnauthorized, msgAuth)
	case a.Kind != participants.ActorAdmin || a.GroupID != groupID:
		return errs.Forbidden(msgForbidden)
	}
	return nil
}

func requireParticipant(a participants.Actor, groupID int64) error {
	switch {
	case a.Kind == participants.ActorAnonymous:
		return errs.New(errs.KindUnauthorized, msgAuth)
	case a.Kind != participants.ActorParticipant || a.GroupID != groupID || a.ParticipantID <= 0:
		return errs.Forbidden(msgForbidden)
	}
	return nil
}

func requireMember(a participants.Actor, groupID int64) error {
	if a.Kind == participants.ActorAdmin {
		return requireAdmin(a, groupID)
	}
	return requireParticipant(a, groupID)
}

// PendingRegistrations renvoie le nombre d'inscriptions en attente
// (administrateur).
func (s *Service) PendingRegistrations(ctx context.Context, a participants.Actor, groupID int64) (int, error) {
	if err := requireAdmin(a, groupID); err != nil {
		return 0, err
	}
	return s.pendingCount(ctx, groupID)
}

func (s *Service) pendingCount(ctx context.Context, groupID int64) (int, error) {
	if s.registrations == nil {
		return 0, nil
	}
	n, err := s.registrations.PendingCount(ctx, groupID)
	if err != nil {
		return 0, errs.Wrap(errs.KindInternal, msgNoRegistration, err)
	}
	return n, nil
}

// ResolvePending applique le choix explicite de l'administrateur sur les
// inscriptions en attente : « ignorer » ou « supprimer ».
func (s *Service) ResolvePending(ctx context.Context, a participants.Actor, groupID int64, how ports.RegistrationResolution) error {
	if err := requireAdmin(a, groupID); err != nil {
		return err
	}
	if how != ports.ResolveIgnore && how != ports.ResolveDelete {
		return errs.Validation(msgResolution)
	}
	if s.registrations == nil {
		return nil
	}
	if err := s.registrations.Resolve(ctx, groupID, how); err != nil {
		return errs.Wrap(errs.KindInternal, msgNoRegistration, err)
	}
	return nil
}

func pendingError(n int) error {
	var msg string
	if n == 1 {
		msg = "Le tirage est impossible : 1 inscription en attente n'a pas été traitée. Ignorez-la ou supprimez-la avant de lancer le tirage."
	} else {
		msg = fmt.Sprintf("Le tirage est impossible : %d inscriptions en attente n'ont pas été traitées. Ignorez-les ou supprimez-les avant de lancer le tirage.", n)
	}
	return errs.Conflict(msg).WithCode(CodePendingRegistrations, "count", n)
}

func infeasibleError(err error) error {
	var inf *drawengine.Infeasible
	if errors.As(err, &inf) {
		kind := errs.KindConflict
		if inf.Cause == drawengine.CauseCapNeedsConfirmation {
			kind = errs.KindValidation
		}
		return errs.Wrap(kind, inf.Error(), inf).WithCode(CodeInfeasible,
			"cause", string(inf.Cause), "missing", inf.Missing)
	}
	return errs.Internal(err)
}

// Draw lance le premier tirage (ou le tirage d'un groupe sans tirage actuel).
func (s *Service) Draw(ctx context.Context, a participants.Actor, groupID int64, req Request) (*Outcome, error) {
	if err := requireAdmin(a, groupID); err != nil {
		return nil, err
	}
	if !req.Confirmed {
		return nil, errs.Validation(msgConfirmDraw)
	}
	st, err := s.Status(ctx, groupID)
	if err != nil {
		return nil, err
	}
	if st.Done {
		return nil, errs.Conflict(msgAlreadyDone)
	}
	return s.run(ctx, groupID, req, 0)
}

// Redraw refait le tirage complet : l'ancien est archivé dans l'historique,
// les drapeaux de découverte repartent de zéro, les réglages du groupe ne
// changent pas.
func (s *Service) Redraw(ctx context.Context, a participants.Actor, groupID int64, req Request) (*Outcome, error) {
	if err := requireAdmin(a, groupID); err != nil {
		return nil, err
	}
	if !req.Confirmed {
		return nil, errs.Validation(msgConfirmRedraw)
	}
	st, err := s.Status(ctx, groupID)
	if err != nil {
		return nil, err
	}
	if !st.Done {
		return nil, errs.Conflict(msgNotDone)
	}
	return s.run(ctx, groupID, req, st.DrawID)
}

// input lit le plafond du groupe, les rôles et les contraintes.
func (s *Service) input(ctx context.Context, groupID int64, capConfirmed bool) (drawengine.Input, *participants.Roster, int, error) {
	g, err := s.groups.ByID(ctx, groupID)
	if err != nil {
		return drawengine.Input{}, nil, 0, err
	}
	capN := g.MaxGiftsPerGiver
	if capN <= 0 {
		capN = drawengine.DefaultCap
	}
	ro, err := s.roster.Roster(ctx, groupID)
	if err != nil {
		return drawengine.Input{}, nil, 0, err
	}
	c, err := s.constraints.BuildConstraints(ctx, groupID)
	if err != nil {
		return drawengine.Input{}, nil, 0, err
	}
	in := drawengine.Input{Cap: capN, CapConfirmed: capConfirmed}
	for _, e := range ro.Entries {
		in.Participants = append(in.Participants, drawengine.Participant{ID: e.ID, Role: drawengine.Role(e.Role)})
	}
	for _, p := range c.Exclusions {
		in.Exclusions = append(in.Exclusions, drawengine.Pair{Giver: p.Giver, Receiver: p.Receiver})
	}
	for _, p := range c.Forced {
		in.Forced = append(in.Forced, drawengine.Pair{Giver: p.Giver, Receiver: p.Receiver})
	}
	return in, ro, capN, nil
}

// run exécute le tirage ; replace est l'identifiant du tirage actuel à
// archiver (0 pour un premier tirage).
func (s *Service) run(ctx context.Context, groupID int64, req Request, replace int64) (*Outcome, error) {
	n, err := s.pendingCount(ctx, groupID)
	if err != nil {
		return nil, err
	}
	if n > 0 {
		return nil, pendingError(n)
	}
	in, ro, capN, err := s.input(ctx, groupID, req.CapConfirmed)
	if err != nil {
		return nil, err
	}
	res, err := drawengine.DrawWithOptions(in, s.engine)
	if err != nil {
		return nil, infeasibleError(err)
	}
	now := s.clock.Now().UTC()
	year := now.In(s.loc).Year()
	kind := KindDraw
	if replace != 0 {
		kind = KindRedraw
	}
	drawID, err := s.persist(ctx, groupID, ro, res.Pairs, replace, kind, year, capN, now)
	if err != nil {
		return nil, err
	}
	return &Outcome{DrawID: drawID, Year: year, Report: buildReport(ro, capN, toPairs(res.Pairs))}, nil
}

// persist écrit le tirage dans une seule transaction : archivage de
// l'ancien, nouveau tirage, paires, historique. Toute erreur annule tout.
func (s *Service) persist(ctx context.Context, groupID int64, ro *participants.Roster, pairs []drawengine.Pair,
	replace int64, kind string, year, capN int, now time.Time) (int64, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, errs.Internal(err)
	}
	defer func() { _ = tx.Rollback() }()
	at := formatTime(now)
	if replace != 0 {
		var rev int
		if err := tx.QueryRowContext(ctx, `SELECT revision FROM draws WHERE id = ? AND group_id = ? AND status = 'actuel'`,
			replace, groupID).Scan(&rev); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return 0, errs.Conflict(msgChanged)
			}
			return 0, errs.Internal(err)
		}
		r, err := tx.ExecContext(ctx, `UPDATE draws SET status = 'archive', archived_at = ?
			WHERE id = ? AND group_id = ? AND status = 'actuel'`, at, replace, groupID)
		if err != nil {
			return 0, errs.Internal(err)
		}
		if n, _ := r.RowsAffected(); n != 1 {
			return 0, errs.Conflict(msgChanged)
		}
		if err := insertEvent(ctx, tx, groupID, replace, KindArchive, rev, at); err != nil {
			return 0, err
		}
	}
	if err := sameRoster(ctx, tx, groupID, ro); err != nil {
		return 0, err
	}
	r, err := tx.ExecContext(ctx, `INSERT INTO draws (group_id, year, kind, status, revision, gift_cap, drawn_at)
		VALUES (?, ?, ?, 'actuel', 1, ?, ?)`, groupID, year, kind, capN, at)
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE") {
			return 0, errs.Conflict(msgAlreadyDone)
		}
		return 0, errs.Internal(err)
	}
	drawID, err := r.LastInsertId()
	if err != nil {
		return 0, errs.Internal(err)
	}
	if err := insertPairs(ctx, tx, drawID, toPairs(pairs)); err != nil {
		return 0, err
	}
	if err := insertEvent(ctx, tx, groupID, drawID, kind, 1, at); err != nil {
		return 0, err
	}
	if err := tx.Commit(); err != nil {
		if strings.Contains(err.Error(), "UNIQUE") {
			return 0, errs.Conflict(msgAlreadyDone)
		}
		return 0, errs.Internal(err)
	}
	return drawID, nil
}

func toPairs(in []drawengine.Pair) []Pair {
	out := make([]Pair, len(in))
	for i, p := range in {
		out[i] = Pair{Giver: p.Giver, Receiver: p.Receiver}
	}
	return out
}

func insertPairs(ctx context.Context, tx *sql.Tx, drawID int64, pairs []Pair) error {
	stmt, err := tx.PrepareContext(ctx, `INSERT INTO draw_pairs (draw_id, giver_id, receiver_id) VALUES (?, ?, ?)`)
	if err != nil {
		return errs.Internal(err)
	}
	defer stmt.Close()
	for _, p := range pairs {
		if _, err := stmt.ExecContext(ctx, drawID, p.Giver, p.Receiver); err != nil {
			return errs.Internal(err)
		}
	}
	return nil
}

func insertEvent(ctx context.Context, tx *sql.Tx, groupID, drawID int64, kind string, revision int, at string) error {
	if _, err := tx.ExecContext(ctx, `INSERT INTO draw_events (group_id, draw_id, kind, revision, at) VALUES (?, ?, ?, ?, ?)`,
		groupID, drawID, kind, revision, at); err != nil {
		return errs.Internal(err)
	}
	return nil
}

// sameRoster vérifie, dans la transaction, que les participants n'ont pas
// changé depuis la lecture qui a servi au tirage.
func sameRoster(ctx context.Context, tx *sql.Tx, groupID int64, ro *participants.Roster) error {
	rows, err := tx.QueryContext(ctx, `SELECT id, draw_role FROM participants WHERE group_id = ? ORDER BY id`, groupID)
	if err != nil {
		return errs.Internal(err)
	}
	defer rows.Close()
	want := append([]participants.RosterEntry(nil), ro.Entries...)
	sort.Slice(want, func(i, j int) bool { return want[i].ID < want[j].ID })
	i := 0
	for rows.Next() {
		var id int64
		var role string
		if err := rows.Scan(&id, &role); err != nil {
			return errs.Internal(err)
		}
		if i >= len(want) || want[i].ID != id || string(want[i].Role) != role {
			return errs.Conflict(msgRosterChanged)
		}
		i++
	}
	if err := rows.Err(); err != nil {
		return errs.Internal(err)
	}
	if i != len(want) {
		return errs.Conflict(msgRosterChanged)
	}
	return nil
}
