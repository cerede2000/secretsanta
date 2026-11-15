// Package adjust ajuste le tirage après coup (F4c.1) : retrait ou ajout d'une
// personne avec le moins de paires changées possible, aperçu sans aucun nom,
// courriels aux seules personnes concernées, archivage de la lettre de
// l'ancienne cible et historique.
//
// Règle de confidentialité : qui offre à qui ne sort jamais de ce paquet.
// L'aperçu et le résultat ne contiennent que des comptes et des textes sans
// nom ; les erreurs ne citent personne ; rien n'est journalisé.
//
// Branchements attendus de l'orchestrateur : DrawWriter (*draw.Service),
// RosterSource, ConstraintSource et GroupSource (comme pour draw), Directory
// (ParticipantDirectory), ports.EmailSender (F6), Archiver (F5b), Remover
// (retrait de la ligne du participant et des règles qui le citent), Composer
// (facultatif).
package adjust

import (
	"context"
	"database/sql"
	"errors"

	"secretsanta/internal/draw"
	"secretsanta/internal/drawengine"
	"secretsanta/internal/participants"
	"secretsanta/internal/shared/errs"
	"secretsanta/internal/shared/ports"
)

// Kind est la nature d'un ajustement.
type Kind string

const (
	// KindRemove retire une personne du tirage.
	KindRemove Kind = "retrait"
	// KindAdd ajoute au tirage une personne déjà créée dans le groupe.
	KindAdd Kind = "ajout"
)

// Change est l'ajustement demandé.
type Change struct {
	Kind          Kind
	ParticipantID int64
}

// Refusal explique pourquoi aucun ajustement n'est possible. Aucun nom :
// seulement une cause, un message et des propositions en français.
type Refusal struct {
	Cause drawengine.Cause
	// Capacity indique une capacité de cadeaux insuffisante (trop de
	// personnes ne peuvent ou ne veulent pas offrir plus d'un cadeau,
	// plafond trop bas, trop peu de tireurs).
	Capacity bool
	// Missing est le nombre de cadeaux manquants (capacité).
	Missing     int
	Message     string
	Suggestions []string
	// ProposeFullDraw propose un tirage complet (l'ancien sera archivé).
	ProposeFullDraw bool
}

func (r *Refusal) Error() string { return r.Message }

// Preview est l'aperçu montré à l'administrateur : uniquement des comptes.
type Preview struct {
	Kind Kind
	// Possible est faux quand aucun ajustement n'existe ; Refusal explique.
	Possible bool
	// Affected est le nombre de personnes déjà dans le tirage dont les
	// cibles changent (la personne ajoutée n'est pas comptée).
	Affected int
	// AlreadyDiscovered est, parmi elles, le nombre de celles qui avaient
	// déjà découvert la cible qu'elles perdent.
	AlreadyDiscovered int
	// ChangedPairs est le nombre de paires nouvelles.
	ChangedPairs int
	// Revision est la révision du tirage lue pour l'aperçu, à renvoyer à
	// Apply pour confirmer.
	Revision int
	// Summary est le texte français de l'aperçu, sans nom.
	Summary string
	Refusal *Refusal
}

// Confirmation accompagne l'application d'un ajustement.
type Confirmation struct {
	// Confirmed est l'accord explicite de l'administrateur, exigé.
	Confirmed bool
	// Revision est celle de l'aperçu ; si le tirage a changé depuis,
	// l'ajustement est refusé.
	Revision int
}

// Result est le résultat d'un ajustement appliqué : comptes seulement.
type Result struct {
	Preview
	// Notified est le nombre de personnes prévenues par courriel.
	Notified int
	// NotifyFailed est le nombre de courriels qui n'ont pas pu partir
	// (l'ajustement est fait).
	NotifyFailed int
}

// Deps sont les dépendances du service.
type Deps struct {
	Draw        DrawWriter
	Groups      draw.GroupSource
	Roster      draw.RosterSource
	Constraints draw.ConstraintSource
	Directory   Directory
	Mail        ports.EmailSender
	Archiver    Archiver
	Remover     Remover
	// Compose écrit les courriels ; nil vaut DefaultComposer.
	Compose Composer
}

// Service ajuste le tirage.
type Service struct {
	db    *sql.DB
	state *draw.State
	deps  Deps
}

// NewService construit le service d'ajustement.
func NewService(d *sql.DB, deps Deps) *Service {
	if deps.Compose == nil {
		deps.Compose = DefaultComposer
	}
	return &Service{db: d, state: draw.NewState(d), deps: deps}
}

func requireAdmin(a participants.Actor, groupID int64) error {
	switch {
	case a.Kind == participants.ActorAnonymous:
		return errs.New(errs.KindUnauthorized, msgAuth)
	case a.Kind != participants.ActorAdmin || a.GroupID != groupID || groupID == 0:
		return errs.Forbidden(msgForbidden)
	}
	return nil
}

// plan est un ajustement calculé, prêt à écrire.
type plan struct {
	drawID   int64
	revision int
	current  []CurrentPair
	next     []Pair
	impact   impact
	preview  Preview
}

// Preview calcule l'aperçu d'un ajustement sans rien modifier.
func (s *Service) Preview(ctx context.Context, a participants.Actor, groupID int64, ch Change) (*Preview, error) {
	if err := requireAdmin(a, groupID); err != nil {
		return nil, err
	}
	pl, err := s.plan(ctx, groupID, ch)
	if err != nil {
		return nil, err
	}
	p := pl.preview
	return &p, nil
}

// Apply applique l'ajustement confirmé : archivage des lettres des anciennes
// cibles, remplacement des paires (les paires inchangées gardent leur
// drapeau de découverte), historique et retrait éventuel du participant,
// dans une seule transaction ; puis courriel aux seules personnes dont les
// cibles changent. Sans ajustement possible, rien n'est modifié et l'erreur
// porte un *Refusal.
func (s *Service) Apply(ctx context.Context, a participants.Actor, groupID int64, ch Change, c Confirmation) (*Result, error) {
	if err := requireAdmin(a, groupID); err != nil {
		return nil, err
	}
	if !c.Confirmed {
		return nil, errs.Validation(msgConfirm)
	}
	if ch.Kind == KindRemove && s.deps.Remover == nil {
		return nil, errs.Internal(errors.New(msgNoRemover))
	}
	pl, err := s.plan(ctx, groupID, ch)
	if err != nil {
		return nil, err
	}
	if !pl.preview.Possible {
		return nil, refusalError(pl.preview.Refusal)
	}
	if c.Revision != pl.revision {
		return nil, errs.Conflict(msgChanged)
	}
	if err := s.write(ctx, groupID, ch, pl); err != nil {
		return nil, err
	}
	res := &Result{Preview: pl.preview}
	res.Revision = pl.revision + 1
	for _, id := range pl.impact.notify {
		if s.notify(ctx, groupID, id, pl.impact.gained[id]) {
			res.Notified++
		} else {
			res.NotifyFailed++
		}
	}
	return res, nil
}

func refusalError(r *Refusal) error {
	code := CodeImpossible
	if r.Capacity {
		code = CodeCapacity
	}
	return errs.Wrap(errs.KindConflict, r.Message, r).WithCode(code,
		"cause", string(r.Cause), "missing", r.Missing)
}

// write écrit l'ajustement dans une seule transaction, après avoir vérifié
// que le tirage n'a pas changé depuis le calcul.
func (s *Service) write(ctx context.Context, groupID int64, ch Change, pl *plan) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return errs.Internal(err)
	}
	defer func() { _ = tx.Rollback() }()
	var drawID int64
	var rev int
	err = tx.QueryRowContext(ctx, `SELECT id, revision FROM draws WHERE group_id = ? AND status = 'actuel'`, groupID).
		Scan(&drawID, &rev)
	if errors.Is(err, sql.ErrNoRows) {
		return errs.Conflict(msgChanged)
	}
	if err != nil {
		return errs.Internal(err)
	}
	if drawID != pl.drawID || rev != pl.revision {
		return errs.Conflict(msgChanged)
	}
	now, err := readPairs(ctx, tx, drawID)
	if err != nil {
		return err
	}
	if !samePairs(now, pl.current) {
		return errs.Conflict(msgChanged)
	}
	if s.deps.Archiver != nil {
		for _, p := range pl.impact.lost {
			if err := s.deps.Archiver.ArchiveTx(ctx, tx, groupID, p.Giver, p.Receiver); err != nil {
				return errs.Internal(err)
			}
		}
	}
	out := make([]draw.Pair, len(pl.next))
	for i, p := range pl.next {
		out[i] = draw.Pair{Giver: p.Giver, Receiver: p.Receiver}
	}
	if err := s.deps.Draw.AdjustTx(ctx, tx, groupID, out); err != nil {
		return err
	}
	if ch.Kind == KindRemove {
		if err := s.deps.Remover.RemoveTx(ctx, tx, groupID, ch.ParticipantID); err != nil {
			return err
		}
	}
	if err := tx.Commit(); err != nil {
		return errs.Internal(err)
	}
	return nil
}

// notify prévient une personne ; faux si le courriel n'a pas pu partir.
func (s *Service) notify(ctx context.Context, groupID, id int64, gained bool) bool {
	if s.deps.Directory == nil || s.deps.Mail == nil {
		return false
	}
	c, err := s.deps.Directory.Contact(ctx, groupID, id)
	if err != nil || c.Email == "" {
		return false
	}
	msg, err := s.deps.Compose(ctx, Notice{GroupID: groupID, ParticipantID: id, Contact: c, NewTarget: gained})
	if err != nil {
		return false
	}
	return s.deps.Mail.Send(ctx, msg) == nil
}

type queryer interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
}

// readPairs lit les paires d'un tirage et leurs drapeaux de découverte
// (usage interne au serveur).
func readPairs(ctx context.Context, q queryer, drawID int64) ([]CurrentPair, error) {
	rows, err := q.QueryContext(ctx, `SELECT giver_id, receiver_id, discovered_at IS NOT NULL FROM draw_pairs
		WHERE draw_id = ? ORDER BY giver_id, receiver_id`, drawID)
	if err != nil {
		return nil, errs.Internal(err)
	}
	defer rows.Close()
	var out []CurrentPair
	for rows.Next() {
		var p CurrentPair
		if err := rows.Scan(&p.Giver, &p.Receiver, &p.Discovered); err != nil {
			return nil, errs.Internal(err)
		}
		out = append(out, p)
	}
	if err := rows.Err(); err != nil {
		return nil, errs.Internal(err)
	}
	return out, nil
}

func samePairs(a, b []CurrentPair) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// plan lit le tirage actuel, les rôles, les règles et le plafond, puis
// calcule l'ajustement. Toutes les lectures se font hors transaction ;
// write revérifie le tirage avant d'écrire.
func (s *Service) plan(ctx context.Context, groupID int64, ch Change) (*plan, error) {
	if ch.Kind != KindRemove && ch.Kind != KindAdd {
		return nil, errs.Validation(msgUnknownKind)
	}
	st, err := s.state.Status(ctx, groupID)
	if err != nil {
		return nil, err
	}
	if !st.Done {
		return nil, errs.Conflict(msgNotDone)
	}
	current, err := readPairs(ctx, s.db, st.DrawID)
	if err != nil {
		return nil, err
	}
	g, err := s.deps.Groups.ByID(ctx, groupID)
	if err != nil {
		return nil, err
	}
	ro, err := s.deps.Roster.Roster(ctx, groupID)
	if err != nil {
		return nil, err
	}
	cons, err := s.deps.Constraints.BuildConstraints(ctx, groupID)
	if err != nil {
		return nil, err
	}
	roles := map[int64]drawengine.Role{}
	for _, e := range ro.Entries {
		roles[e.ID] = drawengine.Role(e.Role)
	}
	inDraw := map[int64]bool{}
	for _, p := range current {
		inDraw[p.Receiver] = true
	}
	if _, ok := roles[ch.ParticipantID]; !ok || ch.ParticipantID <= 0 {
		return nil, errs.NotFound(msgNotInGroup)
	}
	var removed, added int64
	switch ch.Kind {
	case KindRemove:
		if !inDraw[ch.ParticipantID] {
			return nil, errs.Conflict(msgNotInDraw)
		}
		delete(inDraw, ch.ParticipantID)
		removed = ch.ParticipantID
	case KindAdd:
		if inDraw[ch.ParticipantID] {
			return nil, errs.Conflict(msgAlreadyInDraw)
		}
		inDraw[ch.ParticipantID] = true
		added = ch.ParticipantID
	}
	prob := Problem{Current: current, Cap: g.MaxGiftsPerGiver}
	for _, e := range ro.Entries {
		if inDraw[e.ID] {
			prob.People = append(prob.People, Person{ID: e.ID, Role: drawengine.Role(e.Role)})
		}
	}
	for id := range inDraw {
		if _, ok := roles[id]; !ok {
			// Une personne du tirage n'existe plus : le tirage est incohérent.
			return nil, errs.Conflict(msgChanged)
		}
	}
	for _, p := range cons.Exclusions {
		prob.Exclusions = append(prob.Exclusions, Pair{p.Giver, p.Receiver})
	}
	for _, p := range cons.Forced {
		prob.Forced = append(prob.Forced, Pair{p.Giver, p.Receiver})
	}
	pl := &plan{drawID: st.DrawID, revision: st.Revision, current: current}
	pl.preview = Preview{Kind: ch.Kind, Revision: st.Revision}
	sol, err := Solve(prob)
	if err != nil {
		if !errors.Is(err, errNoSolution) {
			return nil, errs.Internal(err)
		}
		pl.preview.Refusal = explain(prob)
		pl.preview.Summary = pl.preview.Refusal.Message
		return pl, nil
	}
	pl.next = sol.Pairs
	pl.impact = measure(current, sol.Pairs, removed, added)
	pl.preview.Possible = true
	pl.preview.Affected = len(pl.impact.affected)
	pl.preview.AlreadyDiscovered = len(pl.impact.revealed)
	pl.preview.ChangedPairs = pl.impact.changed
	pl.preview.Summary = summary(pl.preview.Affected, pl.preview.AlreadyDiscovered)
	return pl, nil
}

// explain dit, sans aucun nom, pourquoi aucun ajustement n'existe. Le
// moteur de tirage diagnostique la même instance (ses contraintes sont plus
// strictes que celles de l'ajustement : s'il n'y a pas d'ajustement, il n'y
// a pas non plus de tirage).
func explain(p Problem) *Refusal {
	present := map[int64]bool{}
	in := drawengine.Input{Cap: p.Cap, CapConfirmed: true}
	for _, pe := range p.People {
		present[pe.ID] = true
		in.Participants = append(in.Participants, drawengine.Participant{ID: pe.ID, Role: pe.Role})
	}
	for _, e := range p.Exclusions {
		in.Exclusions = append(in.Exclusions, drawengine.Pair{Giver: e.Giver, Receiver: e.Receiver})
	}
	for _, f := range p.Forced {
		if present[f.Giver] && present[f.Receiver] {
			in.Forced = append(in.Forced, drawengine.Pair{Giver: f.Giver, Receiver: f.Receiver})
		}
	}
	r := &Refusal{Cause: drawengine.CauseTooConstrained, Message: msgNoFix}
	var inf *drawengine.Infeasible
	if err := drawengine.Check(in); errors.As(err, &inf) {
		r.Cause, r.Missing = inf.Cause, inf.Missing
		r.Message = msgNoFix + " " + inf.Message
		r.Suggestions = append(r.Suggestions, inf.Suggestions...)
	}
	switch r.Cause {
	case drawengine.CauseTooManyOneGift, drawengine.CauseCapTooLow, drawengine.CauseTooFewGivers:
		r.Capacity = true
	default:
		r.ProposeFullDraw = true
		r.Suggestions = append(r.Suggestions, msgFullDraw)
	}
	return r
}
