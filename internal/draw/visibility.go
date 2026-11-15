package draw

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"secretsanta/internal/participants"
	"secretsanta/internal/shared/errs"
)

func errNotDone() error { return errs.Conflict(msgNotDone) }

func internal(err error) error { return errs.Internal(err) }

// drawPairs lit les paires d'un tirage (usage interne, jamais renvoyé tel
// quel hors du paquet).
func (s *Service) drawPairs(ctx context.Context, drawID int64) ([]Pair, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT giver_id, receiver_id FROM draw_pairs WHERE draw_id = ? ORDER BY id`, drawID)
	if err != nil {
		return nil, internal(err)
	}
	defer rows.Close()
	var out []Pair
	for rows.Next() {
		var p Pair
		if err := rows.Scan(&p.Giver, &p.Receiver); err != nil {
			return nil, internal(err)
		}
		out = append(out, p)
	}
	if err := rows.Err(); err != nil {
		return nil, internal(err)
	}
	return out, nil
}

// Target est une cible de la personne connectée.
type Target struct {
	// PairID identifie la cible (drapeau de découverte).
	PairID     int64
	ReceiverID int64
	Discovered bool
}

// MyTargets est la réponse à « quelles sont mes cibles ? ».
type MyTargets struct {
	// Done indique que le tirage est fait.
	Done bool
	// Ready indique que la révélation est ouverte ; faux, Targets est vide.
	Ready bool
	// Targets sont les cibles de la personne : zéro (réception seule), une
	// ou deux (au plus le plafond).
	Targets []Target
}

// MyTargets renvoie les cibles de la personne connectée, et seulement les
// siennes ; rien tant que la révélation n'est pas ouverte.
func (s *Service) MyTargets(ctx context.Context, a participants.Actor, groupID int64) (*MyTargets, error) {
	if err := requireParticipant(a, groupID); err != nil {
		return nil, err
	}
	st, err := s.Status(ctx, groupID)
	if err != nil {
		return nil, err
	}
	out := &MyTargets{Done: st.Done}
	if !st.Done {
		return out, nil
	}
	open, err := s.gate.IsOpen(ctx, groupID)
	if err != nil {
		return nil, internal(err)
	}
	if !open {
		return out, nil
	}
	out.Ready = true
	rows, err := s.db.QueryContext(ctx, `SELECT id, receiver_id, discovered_at IS NOT NULL FROM draw_pairs
		WHERE draw_id = ? AND giver_id = ? ORDER BY id`, st.DrawID, a.ParticipantID)
	if err != nil {
		return nil, internal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var t Target
		if err := rows.Scan(&t.PairID, &t.ReceiverID, &t.Discovered); err != nil {
			return nil, internal(err)
		}
		out.Targets = append(out.Targets, t)
	}
	if err := rows.Err(); err != nil {
		return nil, internal(err)
	}
	return out, nil
}

// MarkDiscovered pose le drapeau serveur de découverte d'une des cibles de
// la personne connectée, une seule fois ; first est vrai à la première
// pose. La découverte ne revient jamais en arrière.
func (s *Service) MarkDiscovered(ctx context.Context, a participants.Actor, groupID, pairID int64) (first bool, err error) {
	if err := requireParticipant(a, groupID); err != nil {
		return false, err
	}
	st, err := s.Status(ctx, groupID)
	if err != nil {
		return false, err
	}
	if !st.Done {
		return false, errNotDone()
	}
	open, err := s.gate.IsOpen(ctx, groupID)
	if err != nil {
		return false, internal(err)
	}
	if !open {
		return false, errs.Conflict(msgNotOpen)
	}
	var discovered sql.NullString
	err = s.db.QueryRowContext(ctx, `SELECT discovered_at FROM draw_pairs WHERE id = ? AND draw_id = ? AND giver_id = ?`,
		pairID, st.DrawID, a.ParticipantID).Scan(&discovered)
	if errors.Is(err, sql.ErrNoRows) {
		return false, errs.NotFound(msgNotYourTarget)
	}
	if err != nil {
		return false, internal(err)
	}
	if discovered.Valid {
		return false, nil
	}
	r, err := s.db.ExecContext(ctx, `UPDATE draw_pairs SET discovered_at = ?
		WHERE id = ? AND draw_id = ? AND giver_id = ? AND discovered_at IS NULL`,
		formatTime(s.clock.Now()), pairID, st.DrawID, a.ParticipantID)
	if err != nil {
		return false, internal(err)
	}
	n, _ := r.RowsAffected()
	return n == 1, nil
}

// Facts sont les faits exposés à l'administrateur, sans aucune paire.
type Facts struct {
	Status
	// Targets est le nombre de cibles du tirage actuel.
	Targets int
	// Discovered est le nombre de cibles déjà découvertes.
	Discovered int
}

// AdminFacts renvoie les faits du tirage actuel à l'administrateur.
func (s *Service) AdminFacts(ctx context.Context, a participants.Actor, groupID int64) (*Facts, error) {
	if err := requireAdmin(a, groupID); err != nil {
		return nil, err
	}
	st, err := s.Status(ctx, groupID)
	if err != nil {
		return nil, err
	}
	f := &Facts{Status: st}
	if !st.Done {
		return f, nil
	}
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*), COUNT(discovered_at) FROM draw_pairs WHERE draw_id = ?`,
		st.DrawID).Scan(&f.Targets, &f.Discovered); err != nil {
		return nil, internal(err)
	}
	return f, nil
}

// TargetCounts renvoie, pour chaque participant du tirage actuel, son
// nombre de cibles (0 en réception seule, 1 ou 2), sans aucun nom ni
// paire ; nil sans tirage. Port interne pour le courriel « tirage prêt ».
func (s *Service) TargetCounts(ctx context.Context, groupID int64) (map[int64]int, error) {
	st, err := s.Status(ctx, groupID)
	if err != nil || !st.Done {
		return nil, err
	}
	pairs, err := s.drawPairs(ctx, st.DrawID)
	if err != nil {
		return nil, err
	}
	out := make(map[int64]int, len(pairs))
	for _, p := range pairs {
		if _, ok := out[p.Receiver]; !ok {
			out[p.Receiver] = 0
		}
	}
	for _, p := range pairs {
		out[p.Giver]++
	}
	return out, nil
}

// HistoryEvent est une ligne du journal d'un tirage.
type HistoryEvent struct {
	Kind     string
	Revision int
	At       time.Time
}

// HistoryEntry est un tirage de l'historique. MyTargets ne contient que les
// destinataires de la personne connectée (jamais pour l'administrateur).
type HistoryEntry struct {
	DrawID     int64
	Year       int
	Kind       string
	Archived   bool
	DrawnAt    time.Time
	ArchivedAt *time.Time
	Revision   int
	PairCount  int
	Events     []HistoryEvent
	MyTargets  []int64
}

// History renvoie l'historique des tirages du groupe, du plus récent au plus
// ancien. L'administrateur n'y voit aucune paire ; un participant n'y voit
// que ses propres destinataires, et rien pour l'année du tirage actuel
// tant que la révélation n'est pas ouverte.
func (s *Service) History(ctx context.Context, a participants.Actor, groupID int64) ([]HistoryEntry, error) {
	if err := requireMember(a, groupID); err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx, `SELECT d.id, d.year, d.kind, d.status, d.drawn_at, d.archived_at, d.revision,
		(SELECT COUNT(*) FROM draw_pairs p WHERE p.draw_id = d.id)
		FROM draws d WHERE d.group_id = ? ORDER BY d.id DESC`, groupID)
	if err != nil {
		return nil, internal(err)
	}
	var out []HistoryEntry
	for rows.Next() {
		var e HistoryEntry
		var status, drawn string
		var archived sql.NullString
		if err := rows.Scan(&e.DrawID, &e.Year, &e.Kind, &status, &drawn, &archived, &e.Revision, &e.PairCount); err != nil {
			rows.Close()
			return nil, internal(err)
		}
		e.Archived = status == statusArchived
		if e.DrawnAt, err = parseTime(drawn); err != nil {
			rows.Close()
			return nil, internal(err)
		}
		if archived.Valid {
			t, err := parseTime(archived.String)
			if err != nil {
				rows.Close()
				return nil, internal(err)
			}
			e.ArchivedAt = &t
		}
		out = append(out, e)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, internal(err)
	}
	rows.Close()
	for i := range out {
		if out[i].Events, err = s.events(ctx, out[i].DrawID); err != nil {
			return nil, err
		}
	}
	if a.Kind != participants.ActorParticipant {
		return out, nil
	}
	hidden := map[int]bool{}
	st, err := s.Status(ctx, groupID)
	if err != nil {
		return nil, err
	}
	if st.Done {
		open, err := s.gate.IsOpen(ctx, groupID)
		if err != nil {
			return nil, internal(err)
		}
		if !open {
			hidden[st.Year] = true
		}
	}
	for i := range out {
		if hidden[out[i].Year] {
			continue
		}
		if out[i].MyTargets, err = s.receiversOf(ctx, out[i].DrawID, a.ParticipantID); err != nil {
			return nil, err
		}
	}
	return out, nil
}

func (s *Service) events(ctx context.Context, drawID int64) ([]HistoryEvent, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT kind, revision, at FROM draw_events WHERE draw_id = ? ORDER BY id`, drawID)
	if err != nil {
		return nil, internal(err)
	}
	defer rows.Close()
	var out []HistoryEvent
	for rows.Next() {
		var ev HistoryEvent
		var at string
		if err := rows.Scan(&ev.Kind, &ev.Revision, &at); err != nil {
			return nil, internal(err)
		}
		if ev.At, err = parseTime(at); err != nil {
			return nil, internal(err)
		}
		out = append(out, ev)
	}
	if err := rows.Err(); err != nil {
		return nil, internal(err)
	}
	return out, nil
}

func (s *Service) receiversOf(ctx context.Context, drawID, giver int64) ([]int64, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT receiver_id FROM draw_pairs WHERE draw_id = ? AND giver_id = ? ORDER BY id`,
		drawID, giver)
	if err != nil {
		return nil, internal(err)
	}
	defer rows.Close()
	var out []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, internal(err)
		}
		out = append(out, id)
	}
	if err := rows.Err(); err != nil {
		return nil, internal(err)
	}
	return out, nil
}
