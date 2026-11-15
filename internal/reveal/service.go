package reveal

import (
	"context"
	"database/sql"
	"errors"
	"sort"
	"strings"
	"sync"
	"time"

	"secretsanta/internal/participants"
	"secretsanta/internal/shared/clock"
	"secretsanta/internal/shared/errs"
)

// DefaultRetryAfter est le délai avant de retenter une ouverture ou un envoi
// d'avis en échec.
const DefaultRetryAfter = time.Minute

// Options règle le service.
type Options struct {
	// RetryAfter est le délai avant une nouvelle tentative ; 0 vaut
	// DefaultRetryAfter.
	RetryAfter time.Duration
}

// Service fait vivre la révélation.
type Service struct {
	db     *sql.DB
	clock  clock.Clock
	sched  Scheduler
	draws  DrawSource
	mailer ReadyMailer
	retry  time.Duration

	mu    sync.Mutex
	tasks map[int64]*task
	// flushRetry vaut vrai quand une reprise de Flush est déjà planifiée.
	flushRetry bool

	flushMu sync.Mutex
}

// task est une tâche d'ouverture planifiée pour un groupe.
type task struct{ id uint64 }

// NewService construit le service de la révélation. mailer peut être nil :
// les avis restent alors en attente jusqu'au branchement de la file.
func NewService(d *sql.DB, c clock.Clock, sched Scheduler, draws DrawSource, mailer ReadyMailer, opts Options) *Service {
	s := &Service{db: d, clock: c, sched: sched, draws: draws, mailer: mailer, retry: opts.RetryAfter,
		tasks: map[int64]*task{}}
	if s.retry <= 0 {
		s.retry = DefaultRetryAfter
	}
	return s
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

// fail traduit une erreur de lecture : groupe inconnu ou erreur interne.
func fail(err error) error {
	if errors.Is(err, errNoGroup) {
		return errs.NotFound(msgNoGroup)
	}
	var e *errs.Error
	if errors.As(err, &e) {
		return err
	}
	return errs.Internal(err)
}

// begin ouvre une transaction et prend aussitôt le verrou d'écriture par une
// écriture sur la ligne du groupe : les lectures qui suivent voient l'état le
// plus récent et aucune autre écriture ne peut s'intercaler.
func (s *Service) begin(ctx context.Context, groupID int64) (*sql.Tx, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, errs.Internal(err)
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO reveal_groups (group_id, changes) VALUES (?, 0)
		ON CONFLICT (group_id) DO UPDATE SET changes = changes + 1`, groupID)
	if err != nil {
		_ = tx.Rollback()
		if strings.Contains(err.Error(), "FOREIGN KEY") {
			return nil, errs.NotFound(msgNoGroup)
		}
		return nil, errs.Internal(err)
	}
	return tx, nil
}

// SetRevealDate modifie la date de révélation du groupe (administrateur).
// La date doit être dans le futur. Elle est refusée une fois la révélation
// ouverte (ouverture enregistrée, groupe sans date ou date atteinte) ou dès
// qu'une cible du tirage actuel a été découverte ; aucune découverte n'est
// touchée. at nil retire la date, seulement avant le tirage. La tâche
// d'ouverture est replanifiée.
func (s *Service) SetRevealDate(ctx context.Context, a participants.Actor, groupID int64, at *time.Time) error {
	if err := requireAdmin(a, groupID); err != nil {
		return err
	}
	now := s.clock.Now()
	var value any
	var when time.Time
	if at != nil {
		when = at.UTC().Truncate(time.Second)
		if when.Year() > 9999 {
			return errs.Validation(msgDateInvalid)
		}
		if !when.After(now) {
			return errs.Validation(msgDatePast)
		}
		value = when.Format(time.RFC3339)
	}
	tx, err := s.begin(ctx, groupID)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	st, err := readState(ctx, tx, now, groupID)
	if err != nil {
		return fail(err)
	}
	if st.locked() {
		return errs.Conflict(msgDateLocked).WithCode(CodeDateLocked)
	}
	if at == nil && st.draw.ok {
		return errs.Validation(msgDateRequired)
	}
	r, err := tx.ExecContext(ctx, `UPDATE groups SET reveal_at = ? WHERE id = ?`, value, groupID)
	if err != nil {
		return errs.Internal(err)
	}
	if n, _ := r.RowsAffected(); n != 1 {
		return errs.NotFound(msgNoGroup)
	}
	if err := tx.Commit(); err != nil {
		return errs.Internal(err)
	}
	if at == nil {
		s.cancel(groupID)
		return nil
	}
	s.schedule(groupID, when)
	return nil
}

// OpenNow ouvre la révélation du tirage actuel pour tous, tout de suite
// (bouton administrateur « ouvrir maintenant »). confirmed est exigé. La
// date de révélation est ensuite verrouillée. Renvoie vrai si cet appel a
// ouvert la révélation, faux si elle l'était déjà.
func (s *Service) OpenNow(ctx context.Context, a participants.Actor, groupID int64, confirmed bool) (bool, error) {
	if err := requireAdmin(a, groupID); err != nil {
		return false, err
	}
	if !confirmed {
		return false, errs.Validation(msgConfirmOpen)
	}
	res, err := s.open(ctx, groupID, HowManual, false)
	if err != nil {
		return false, err
	}
	if !res.drawDone {
		return false, errs.Conflict(msgNotDone)
	}
	return res.newOpening, nil
}

// OnDrawChanged est à appeler par l'orchestrateur après un tirage, un
// nouveau tirage ou un ajustement : si la révélation est due (sans date ou
// date atteinte) ou déjà ouverte pour ce tirage, les cibles non encore
// ouvertes le sont et les avis « tirage prêt » partent ; avec une date à
// venir, la tâche d'ouverture est (re)planifiée à la date.
func (s *Service) OnDrawChanged(ctx context.Context, groupID int64) error {
	at, err := revealAt(ctx, s.db, groupID)
	if err != nil {
		return fail(err)
	}
	if at != nil && s.clock.Now().Before(*at) {
		s.schedule(groupID, *at)
	}
	_, err = s.open(ctx, groupID, "", true)
	return err
}

// Restore replanifie, au démarrage, les ouvertures à venir et rattrape
// celles qui sont dues (date passée pendant un arrêt, tirage sans date non
// encore ouvert), puis reprend les avis en attente.
func (s *Service) Restore(ctx context.Context) error {
	rows, err := s.db.QueryContext(ctx, `SELECT g.id FROM groups g
		LEFT JOIN draws d ON d.group_id = g.id AND d.status = 'actuel'
		WHERE (d.id IS NULL AND g.reveal_at IS NOT NULL)
		   OR (d.id IS NOT NULL AND NOT EXISTS (SELECT 1 FROM reveal_openings o WHERE o.draw_id = d.id))
		ORDER BY g.id`)
	if err != nil {
		return errs.Internal(err)
	}
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return errs.Internal(err)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return errs.Internal(err)
	}
	rows.Close()
	var first error
	for _, id := range ids {
		if err := s.OnDrawChanged(ctx, id); err != nil {
			s.scheduleRetry(id)
			if first == nil {
				first = err
			}
		}
	}
	if err := s.Flush(ctx); err != nil && first == nil {
		first = err
	}
	return first
}

// openResult décrit le résultat d'une ouverture.
type openResult struct {
	drawDone   bool
	newOpening bool
	// newTargets est le nombre de cibles ouvertes par cet appel.
	newTargets int64
}

// open ouvre la révélation du tirage actuel : enregistre l'ouverture (une
// seule fois), ouvre les cibles qui ne le sont pas encore et prépare un avis
// « tirage prêt » par personne et par tirage. Avec onlyIfDue, rien n'est
// fait tant que la révélation n'est pas due ni déjà ouverte ; how vide est
// alors déduit de la date (HowImmediate sans date, HowDate sinon).
func (s *Service) open(ctx context.Context, groupID int64, how string, onlyIfDue bool) (openResult, error) {
	before, err := currentDraw(ctx, s.db, groupID)
	if err != nil {
		return openResult{}, errs.Internal(err)
	}
	if !before.ok {
		if _, err := revealAt(ctx, s.db, groupID); err != nil {
			return openResult{}, fail(err)
		}
		return openResult{}, nil
	}
	counts, err := s.draws.TargetCounts(ctx, groupID)
	if err != nil {
		return openResult{}, fail(err)
	}
	now := s.clock.Now()
	tx, err := s.begin(ctx, groupID)
	if err != nil {
		return openResult{}, err
	}
	defer func() { _ = tx.Rollback() }()
	st, err := readState(ctx, tx, now, groupID)
	if err != nil {
		return openResult{}, fail(err)
	}
	if st.draw != before {
		return openResult{}, errs.Conflict(msgChanged)
	}
	res := openResult{drawDone: true}
	if onlyIfDue && !st.open {
		return res, nil
	}
	if how == "" {
		how = HowDate
		if st.revealAt == nil {
			how = HowImmediate
		}
	}
	at := formatTime(now)
	r, err := tx.ExecContext(ctx, `INSERT INTO reveal_openings (draw_id, group_id, how, opened_at)
		VALUES (?, ?, ?, ?) ON CONFLICT (draw_id) DO NOTHING`, st.draw.id, groupID, how, at)
	if err != nil {
		return openResult{}, errs.Internal(err)
	}
	n, _ := r.RowsAffected()
	res.newOpening = n == 1
	r, err = tx.ExecContext(ctx, `INSERT INTO reveal_targets (pair_id, draw_id, opened_at)
		SELECT id, draw_id, ? FROM draw_pairs WHERE draw_id = ? ORDER BY id
		ON CONFLICT (pair_id) DO NOTHING`, at, st.draw.id)
	if err != nil {
		return openResult{}, errs.Internal(err)
	}
	res.newTargets, _ = r.RowsAffected()
	ids := make([]int64, 0, len(counts))
	for id := range counts {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	for _, id := range ids {
		if _, err := tx.ExecContext(ctx, `INSERT INTO reveal_notices (group_id, draw_id, participant_id, targets, created_at)
			VALUES (?, ?, ?, ?, ?) ON CONFLICT (draw_id, participant_id) DO NOTHING`,
			groupID, st.draw.id, id, counts[id], at); err != nil {
			return openResult{}, errs.Internal(err)
		}
	}
	if err := tx.Commit(); err != nil {
		return openResult{}, errs.Internal(err)
	}
	if err := s.Flush(ctx); err != nil {
		s.scheduleFlushRetry()
	}
	return res, nil
}

// Flush met en file les avis « tirage prêt » en attente du tirage actuel de
// chaque groupe. Un avis n'est marqué envoyé qu'après sa mise en file ; une
// erreur le laisse en attente.
func (s *Service) Flush(ctx context.Context) error {
	if s.mailer == nil {
		return nil
	}
	s.flushMu.Lock()
	defer s.flushMu.Unlock()
	rows, err := s.db.QueryContext(ctx, `SELECT n.id, n.group_id, n.draw_id, n.participant_id, n.targets
		FROM reveal_notices n JOIN draws d ON d.id = n.draw_id AND d.status = 'actuel'
		WHERE n.sent_at IS NULL ORDER BY n.id`)
	if err != nil {
		return errs.Internal(err)
	}
	type pending struct {
		id int64
		n  ReadyNotice
	}
	var list []pending
	for rows.Next() {
		var p pending
		if err := rows.Scan(&p.id, &p.n.GroupID, &p.n.DrawID, &p.n.ParticipantID, &p.n.Targets); err != nil {
			rows.Close()
			return errs.Internal(err)
		}
		p.n.ReceptionOnly = p.n.Targets == 0
		list = append(list, p)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return errs.Internal(err)
	}
	rows.Close()
	var first error
	for _, p := range list {
		if err := s.mailer.EnqueueReady(ctx, p.n); err != nil {
			if first == nil {
				first = errs.Internal(err)
			}
			continue
		}
		if _, err := s.db.ExecContext(ctx, `UPDATE reveal_notices SET sent_at = ? WHERE id = ? AND sent_at IS NULL`,
			formatTime(s.clock.Now()), p.id); err != nil && first == nil {
			first = errs.Internal(err)
		}
	}
	return first
}

// Facts renvoie à l'administrateur les faits de la révélation, sans paire.
func (s *Service) Facts(ctx context.Context, a participants.Actor, groupID int64) (*Facts, error) {
	if err := requireAdmin(a, groupID); err != nil {
		return nil, err
	}
	df, err := s.draws.AdminFacts(ctx, a, groupID)
	if err != nil {
		return nil, fail(err)
	}
	st, err := readState(ctx, s.db, s.clock.Now(), groupID)
	if err != nil {
		return nil, fail(err)
	}
	f := &Facts{
		DrawDone: st.draw.ok, RevealAt: st.revealAt, Open: st.draw.ok && st.open,
		DateLocked: st.locked(), Targets: df.Targets, Discovered: df.Discovered,
	}
	if st.opening.ok {
		at := st.opening.at
		f.OpenedAt = &at
		f.OpenedHow = st.opening.how
	}
	return f, nil
}

// schedule (re)planifie la tâche d'ouverture du groupe à at.
func (s *Service) schedule(groupID int64, at time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if old, ok := s.tasks[groupID]; ok {
		s.sched.Cancel(old.id)
	}
	t := &task{}
	s.tasks[groupID] = t
	t.id = s.sched.At(at, func(ctx context.Context) error { return s.fire(ctx, groupID, t) })
}

// cancel retire la tâche d'ouverture du groupe.
func (s *Service) cancel(groupID int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if old, ok := s.tasks[groupID]; ok {
		s.sched.Cancel(old.id)
		delete(s.tasks, groupID)
	}
}

// fire est la tâche planifiée : elle ouvre la révélation si elle est due et
// n'ouvre que les cibles qui ne le sont pas encore. En cas d'erreur, elle se
// replanifie (sauf si une autre tâche l'a remplacée).
func (s *Service) fire(ctx context.Context, groupID int64, t *task) error {
	s.mu.Lock()
	if s.tasks[groupID] == t {
		delete(s.tasks, groupID)
	}
	s.mu.Unlock()
	if _, err := s.open(ctx, groupID, "", true); err != nil {
		s.scheduleRetry(groupID)
		return err
	}
	return nil
}

// scheduleRetry replanifie l'ouverture après le délai de reprise, sauf si une
// tâche est déjà planifiée pour le groupe.
func (s *Service) scheduleRetry(groupID int64) {
	s.mu.Lock()
	_, busy := s.tasks[groupID]
	s.mu.Unlock()
	if !busy {
		s.schedule(groupID, s.clock.Now().Add(s.retry))
	}
}

// scheduleFlushRetry planifie une reprise des avis en attente.
func (s *Service) scheduleFlushRetry() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.flushRetry {
		return
	}
	s.flushRetry = true
	s.sched.At(s.clock.Now().Add(s.retry), func(ctx context.Context) error {
		s.mu.Lock()
		s.flushRetry = false
		s.mu.Unlock()
		if err := s.Flush(ctx); err != nil {
			s.scheduleFlushRetry()
			return err
		}
		return nil
	})
}
