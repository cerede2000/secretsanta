// Package repair implémente le port ports.RuleChangeRepair (F4c.2) : une
// règle (exclusion, foyer, forçage) ou un changement de rôle décidé après le
// tirage est confronté au tirage actuel. S'il ne contredit aucune paire, rien
// ne change. Sinon l'aperçu ne donne que des comptes (personnes touchées,
// dont déjà découvertes) et la réparation, calculée par adjust.Solve (le
// moins de cibles déjà découvertes changées, puis le moins de paires
// changées, cadeaux supplémentaires d'abord aux offre_plus), s'écrit dans la
// transaction même de la règle. Les seules personnes dont les cibles changent
// reçoivent le courriel de l'ajustement (même gabarit). Sans réparation
// possible, Apply refuse sans rien écrire et l'aperçu le dit
// (Repairable=false) : l'appelant (F3d) conserve alors la règle pour les
// futurs ajustements seulement.
//
// Confidentialité : qui offre à qui ne sort jamais de ce paquet ; l'aperçu
// ne contient que des comptes, les erreurs ne citent personne et rien n'est
// journalisé.
//
// Branchements attendus de l'orchestrateur : DrawWriter (*draw.Service),
// GroupSource, RosterSource, ConstraintSource (comme pour adjust),
// adjust.Directory, ports.EmailSender (F6), adjust.Archiver (F5b, facultatif)
// et un adjust.Composer (facultatif, adjust.DefaultComposer sinon).
package repair

import (
	"context"
	"database/sql"
	"errors"
	"sort"

	"secretsanta/internal/adjust"
	"secretsanta/internal/draw"
	"secretsanta/internal/drawengine"
	"secretsanta/internal/participants"
	"secretsanta/internal/shared/errs"
	"secretsanta/internal/shared/ports"
)

const (
	msgChanged  = "Le tirage a changé entre-temps. Recharge l'aperçu et recommence."
	msgNoRepair = "Aucune réparation du tirage actuel n'est possible sans enfreindre une règle : rien n'a été modifié. Tu peux conserver ce changement pour les futurs ajustements et les nouveaux tirages seulement."
	msgBadRule  = "Cette règle est incomplète ou d'une nature inconnue."
	msgNoWriter = "écriture de règle absente"
	msgNoMember = "Ce participant n'existe pas dans ce groupe."
	msgBadRole  = "Ce rôle n'existe pas : choisis normal, réception seule, offre plus ou un seul."
)

// Deps sont les dépendances du service.
type Deps struct {
	Draw        adjust.DrawWriter
	Groups      draw.GroupSource
	Roster      draw.RosterSource
	Constraints draw.ConstraintSource
	Directory   adjust.Directory
	Mail        ports.EmailSender
	// Archiver archive la lettre de l'ancienne cible (facultatif).
	Archiver adjust.Archiver
	// Compose écrit les courriels ; nil vaut adjust.DefaultComposer.
	Compose adjust.Composer
}

// Service répare le tirage après un changement de règle ou de rôle.
type Service struct {
	db    *sql.DB
	state *draw.State
	deps  Deps
}

var _ ports.RuleChangeRepair = (*Service)(nil)

// NewService construit le service.
func NewService(d *sql.DB, deps Deps) *Service {
	if deps.Compose == nil {
		deps.Compose = adjust.DefaultComposer
	}
	return &Service{db: d, state: draw.NewState(d), deps: deps}
}

// snapshot est l'état lu du tirage actuel et des règles (usage interne).
type snapshot struct {
	done     bool
	drawID   int64
	revision int
	current  []adjust.CurrentPair
	cap      int
	// roles ne contient que les personnes du tirage actuel.
	roles      map[int64]drawengine.Role
	exclusions []adjust.Pair
	forced     []adjust.Pair
}

func validateRule(rule ports.ProposedRule) error {
	switch rule.Kind {
	case ports.RuleExclusion, ports.RuleForcing:
		if len(rule.Members) != 2 || rule.Members[0] == rule.Members[1] {
			return errs.Validation(msgBadRule)
		}
	case ports.RuleHousehold:
		// Un foyer d'une seule personne n'exclut personne.
	case ports.RuleRoleChange:
		if len(rule.Members) != 1 {
			return errs.Validation(msgBadRule)
		}
	default:
		return errs.Validation(msgBadRule)
	}
	return nil
}

// read lit le tirage actuel, le plafond, les rôles et les règles en
// vigueur, hors transaction.
func (s *Service) read(ctx context.Context, groupID int64) (*snapshot, error) {
	st, err := s.state.Status(ctx, groupID)
	if err != nil {
		return nil, err
	}
	sn := &snapshot{done: st.Done, drawID: st.DrawID, revision: st.Revision, roles: map[int64]drawengine.Role{}}
	if !st.Done {
		return sn, nil
	}
	if sn.current, err = readPairs(ctx, s.db, st.DrawID); err != nil {
		return nil, err
	}
	g, err := s.deps.Groups.ByID(ctx, groupID)
	if err != nil {
		return nil, err
	}
	sn.cap = g.MaxGiftsPerGiver
	ro, err := s.deps.Roster.Roster(ctx, groupID)
	if err != nil {
		return nil, err
	}
	all := map[int64]drawengine.Role{}
	for _, e := range ro.Entries {
		all[e.ID] = drawengine.Role(e.Role)
	}
	for _, p := range sn.current {
		r, ok := all[p.Receiver]
		if !ok {
			// Une personne du tirage n'existe plus : tirage incohérent.
			return nil, errs.Conflict(msgChanged)
		}
		sn.roles[p.Receiver] = r
	}
	cons, err := s.deps.Constraints.BuildConstraints(ctx, groupID)
	if err != nil {
		return nil, err
	}
	for _, p := range cons.Exclusions {
		sn.exclusions = append(sn.exclusions, adjust.Pair{Giver: p.Giver, Receiver: p.Receiver})
	}
	for _, p := range cons.Forced {
		sn.forced = append(sn.forced, adjust.Pair{Giver: p.Giver, Receiver: p.Receiver})
	}
	return sn, nil
}

// evaluation est l'effet d'une règle sur le tirage actuel.
type evaluation struct {
	violated   bool
	repairable bool
	next       []adjust.Pair
	im         impact
	// violators sont les tireurs dont une paire contredit la règle ;
	// violRevealed ceux, parmi eux, qui avaient déjà découvert cette paire.
	violators    map[int64]bool
	violRevealed map[int64]bool
}

func (e evaluation) preview() ports.RepairPreview {
	switch {
	case !e.violated:
		return ports.RepairPreview{Repairable: true}
	case !e.repairable:
		return ports.RepairPreview{AffectedCount: len(e.violators), AlreadyDiscoveredCount: len(e.violRevealed)}
	default:
		return ports.RepairPreview{AffectedCount: len(e.im.affected), AlreadyDiscoveredCount: len(e.im.revealed), Repairable: true}
	}
}

// bounds renvoie le nombre minimal et maximal de cadeaux offerts selon le
// rôle (mêmes règles que adjust.Solve).
func bounds(role drawengine.Role, capN int) (int, int, bool) {
	if capN <= 0 {
		capN = drawengine.DefaultCap
	}
	switch role {
	case drawengine.RoleReceptionOnly:
		return 0, 0, true
	case drawengine.RoleOneGift:
		return 1, 1, true
	case drawengine.RoleNormal, drawengine.RoleGivesMore, "":
		return 1, capN, true
	}
	return 0, 0, false
}

// evaluate confronte la règle au tirage actuel ; newRole est le rôle demandé
// pour un changement de rôle (ignoré sinon).
func evaluate(sn *snapshot, rule ports.ProposedRule, newRole drawengine.Role) evaluation {
	ev := evaluation{violators: map[int64]bool{}, violRevealed: map[int64]bool{}}
	if !sn.done {
		return ev
	}
	roles := make(map[int64]drawengine.Role, len(sn.roles))
	for id, r := range sn.roles {
		roles[id] = r
	}
	inDraw := func(id int64) bool { _, ok := roles[id]; return ok }
	mark := func(c adjust.CurrentPair) {
		ev.violated = true
		ev.violators[c.Giver] = true
		if c.Discovered {
			ev.violRevealed[c.Giver] = true
		}
	}
	var extraExcl, extraForced []adjust.Pair
	switch rule.Kind {
	case ports.RuleExclusion:
		g, r := rule.Members[0], rule.Members[1]
		extraExcl = append(extraExcl, adjust.Pair{Giver: g, Receiver: r})
		for _, c := range sn.current {
			if c.Giver == g && c.Receiver == r {
				mark(c)
			}
		}
	case ports.RuleHousehold:
		in := map[int64]bool{}
		for _, m := range rule.Members {
			in[m] = true
		}
		for _, a := range rule.Members {
			for _, b := range rule.Members {
				if a != b {
					extraExcl = append(extraExcl, adjust.Pair{Giver: a, Receiver: b})
				}
			}
		}
		for _, c := range sn.current {
			if in[c.Giver] && in[c.Receiver] {
				mark(c)
			}
		}
	case ports.RuleForcing:
		g, r := rule.Members[0], rule.Members[1]
		extraForced = append(extraForced, adjust.Pair{Giver: g, Receiver: r})
		if inDraw(g) && inDraw(r) {
			for _, c := range sn.current {
				if c.Receiver == r && c.Giver != g {
					mark(c)
				}
			}
		}
	case ports.RuleRoleChange:
		m := rule.Members[0]
		if inDraw(m) {
			roles[m] = newRole
			lo, hi, ok := bounds(newRole, sn.cap)
			var mine []adjust.CurrentPair
			for _, c := range sn.current {
				if c.Giver == m {
					mine = append(mine, c)
				}
			}
			if !ok || len(mine) < lo || len(mine) > hi {
				ev.violated = true
				ev.violators[m] = true
				if len(mine) > hi {
					for _, c := range mine {
						if c.Discovered {
							ev.violRevealed[m] = true
						}
					}
				}
			}
		}
	}
	if !ev.violated {
		return ev
	}
	prob := adjust.Problem{Current: sn.current, Cap: sn.cap,
		Exclusions: append(append([]adjust.Pair(nil), sn.exclusions...), extraExcl...),
		Forced:     append(append([]adjust.Pair(nil), sn.forced...), extraForced...)}
	ids := make([]int64, 0, len(roles))
	for id := range roles {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	for _, id := range ids {
		prob.People = append(prob.People, adjust.Person{ID: id, Role: roles[id]})
	}
	sol, err := adjust.Solve(prob)
	if err != nil {
		return ev
	}
	ev.repairable = true
	ev.next = sol.Pairs
	ev.im = measure(sn.current, sol.Pairs)
	return ev
}

// Preview dit, en comptes seulement, l'effet de la règle sur le tirage
// actuel, sans rien modifier.
//
// Changement de rôle : ports.ProposedRule ne porte pas le rôle demandé.
// L'aperçu est alors prudent : il considère chacun des autres rôles possibles
// et renvoie le plus fort impact parmi ceux qui contredisent le tirage ;
// Repairable n'est faux que si aucun d'eux n'est réparable. Apply calcule
// ensuite la réparation exacte avec le rôle écrit. PreviewRole donne l'aperçu
// exact quand le rôle est connu.
func (s *Service) Preview(ctx context.Context, groupID int64, rule ports.ProposedRule) (ports.RepairPreview, error) {
	if err := validateRule(rule); err != nil {
		return ports.RepairPreview{}, err
	}
	sn, err := s.read(ctx, groupID)
	if err != nil {
		return ports.RepairPreview{}, err
	}
	if rule.Kind != ports.RuleRoleChange {
		return evaluate(sn, rule, "").preview(), nil
	}
	cur, ok := sn.roles[rule.Members[0]]
	if !ok {
		return ports.RepairPreview{Repairable: true}, nil
	}
	if cur == "" {
		cur = drawengine.RoleNormal
	}
	out := ports.RepairPreview{Repairable: true}
	anyViolated, anyRepairable := false, false
	for _, r := range []drawengine.Role{drawengine.RoleNormal, drawengine.RoleReceptionOnly, drawengine.RoleGivesMore, drawengine.RoleOneGift} {
		if r == cur {
			continue
		}
		ev := evaluate(sn, rule, r)
		if !ev.violated {
			continue
		}
		anyViolated = true
		if ev.repairable {
			anyRepairable = true
		}
		p := ev.preview()
		if p.AffectedCount > out.AffectedCount ||
			(p.AffectedCount == out.AffectedCount && p.AlreadyDiscoveredCount > out.AlreadyDiscoveredCount) {
			out.AffectedCount, out.AlreadyDiscoveredCount = p.AffectedCount, p.AlreadyDiscoveredCount
		}
	}
	out.Repairable = !anyViolated || anyRepairable
	return out, nil
}

// PreviewRole donne l'aperçu exact (comptes seulement) du passage de
// participantID au rôle demandé, sans rien modifier.
func (s *Service) PreviewRole(ctx context.Context, groupID, participantID int64, role participants.DrawRole) (ports.RepairPreview, error) {
	if !role.Valid() {
		return ports.RepairPreview{}, errs.Validation(msgBadRole)
	}
	sn, err := s.read(ctx, groupID)
	if err != nil {
		return ports.RepairPreview{}, err
	}
	rule := ports.ProposedRule{Kind: ports.RuleRoleChange, Members: []int64{participantID}}
	return evaluate(sn, rule, drawengine.Role(role)).preview(), nil
}

// Apply écrit la règle (write) puis la réparation du tirage dans une seule
// transaction. Si la règle ne contredit aucune paire, seule la règle est
// écrite. Sans réparation possible, rien n'est écrit et l'erreur est un
// conflit sans nom. Après validation, les seules personnes dont les cibles
// changent sont prévenues (un échec d'envoi n'annule pas la réparation).
func (s *Service) Apply(ctx context.Context, groupID int64, rule ports.ProposedRule, write ports.RuleWriter) error {
	if write == nil {
		return errs.Internal(errors.New(msgNoWriter))
	}
	if err := validateRule(rule); err != nil {
		return err
	}
	sn, err := s.read(ctx, groupID)
	if err != nil {
		return err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return errs.Internal(err)
	}
	defer func() { _ = tx.Rollback() }()
	if err := verifyUnchanged(ctx, tx, groupID, sn); err != nil {
		return err
	}
	if err := write(ctx, tx); err != nil {
		return err
	}
	var role drawengine.Role
	if rule.Kind == ports.RuleRoleChange {
		var r string
		err := tx.QueryRowContext(ctx, `SELECT draw_role FROM participants WHERE group_id = ? AND id = ?`,
			groupID, rule.Members[0]).Scan(&r)
		if errors.Is(err, sql.ErrNoRows) {
			return errs.NotFound(msgNoMember)
		}
		if err != nil {
			return errs.Internal(err)
		}
		role = drawengine.Role(r)
	}
	ev := evaluate(sn, rule, role)
	if ev.violated && !ev.repairable {
		return errs.Conflict(msgNoRepair)
	}
	if ev.violated {
		if s.deps.Archiver != nil {
			for _, p := range ev.im.lost {
				if err := s.deps.Archiver.ArchiveTx(ctx, tx, groupID, p.Giver, p.Receiver); err != nil {
					return errs.Internal(err)
				}
			}
		}
		out := make([]draw.Pair, len(ev.next))
		for i, p := range ev.next {
			out[i] = draw.Pair{Giver: p.Giver, Receiver: p.Receiver}
		}
		if err := s.deps.Draw.AdjustTx(ctx, tx, groupID, out); err != nil {
			return err
		}
	}
	if err := tx.Commit(); err != nil {
		return errs.Internal(err)
	}
	if ev.violated {
		for _, id := range ev.im.affected {
			s.notify(ctx, groupID, id, ev.im.gained[id])
		}
	}
	return nil
}

// verifyUnchanged vérifie, dans la transaction, que le tirage lu n'a pas
// changé (même tirage, même révision, mêmes paires et drapeaux).
func verifyUnchanged(ctx context.Context, tx *sql.Tx, groupID int64, sn *snapshot) error {
	var drawID int64
	var rev int
	err := tx.QueryRowContext(ctx, `SELECT id, revision FROM draws WHERE group_id = ? AND status = 'actuel'`, groupID).
		Scan(&drawID, &rev)
	if errors.Is(err, sql.ErrNoRows) {
		if sn.done {
			return errs.Conflict(msgChanged)
		}
		return nil
	}
	if err != nil {
		return errs.Internal(err)
	}
	if !sn.done || drawID != sn.drawID || rev != sn.revision {
		return errs.Conflict(msgChanged)
	}
	now, err := readPairs(ctx, tx, drawID)
	if err != nil {
		return err
	}
	if len(now) != len(sn.current) {
		return errs.Conflict(msgChanged)
	}
	for i := range now {
		if now[i] != sn.current[i] {
			return errs.Conflict(msgChanged)
		}
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
	msg, err := s.deps.Compose(ctx, adjust.Notice{GroupID: groupID, ParticipantID: id, Contact: c, NewTarget: gained})
	if err != nil {
		return false
	}
	return s.deps.Mail.Send(ctx, msg) == nil
}

type queryer interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
}

// readPairs lit les paires d'un tirage et leurs drapeaux (usage interne).
func readPairs(ctx context.Context, q queryer, drawID int64) ([]adjust.CurrentPair, error) {
	rows, err := q.QueryContext(ctx, `SELECT giver_id, receiver_id, discovered_at IS NOT NULL FROM draw_pairs
		WHERE draw_id = ? ORDER BY giver_id, receiver_id`, drawID)
	if err != nil {
		return nil, errs.Internal(err)
	}
	defer rows.Close()
	var out []adjust.CurrentPair
	for rows.Next() {
		var p adjust.CurrentPair
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

// impact est l'effet d'une réparation (identifiants internes au serveur).
type impact struct {
	// affected sont les tireurs dont les cibles changent, triés.
	affected []int64
	// revealed sont ceux qui perdent une cible déjà découverte.
	revealed map[int64]bool
	// gained sont ceux qui reçoivent une nouvelle cible à découvrir.
	gained map[int64]bool
	// lost sont les paires actuelles abandonnées.
	lost []adjust.Pair
}

func measure(current []adjust.CurrentPair, next []adjust.Pair) impact {
	cur := map[adjust.Pair]bool{}
	for _, c := range current {
		cur[adjust.Pair{Giver: c.Giver, Receiver: c.Receiver}] = true
	}
	nxt := map[adjust.Pair]bool{}
	for _, p := range next {
		nxt[p] = true
	}
	im := impact{revealed: map[int64]bool{}, gained: map[int64]bool{}}
	touched := map[int64]bool{}
	for _, c := range current {
		p := adjust.Pair{Giver: c.Giver, Receiver: c.Receiver}
		if nxt[p] {
			continue
		}
		touched[c.Giver] = true
		im.lost = append(im.lost, p)
		if c.Discovered {
			im.revealed[c.Giver] = true
		}
	}
	for _, p := range next {
		if !cur[p] {
			touched[p.Giver] = true
			im.gained[p.Giver] = true
		}
	}
	for id := range touched {
		im.affected = append(im.affected, id)
	}
	sort.Slice(im.affected, func(i, j int) bool { return im.affected[i] < im.affected[j] })
	return im
}
