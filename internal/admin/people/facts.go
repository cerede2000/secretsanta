package people

import (
	"context"
	"database/sql"
	"sort"
	"strconv"
	"time"

	"secretsanta/internal/shared/errs"
)

type mailState string

const (
	mailNone    mailState = "none"
	mailPending mailState = "pending"
	mailSent    mailState = "sent"
	mailFailed  mailState = "failed"
)

// facts regroupe les faits d'accès du groupe, par personne. Aucune paire :
// « revealed » dit seulement que la personne a découvert toutes ses cibles.
type facts struct {
	mail      map[int64]mailState
	lastSent  map[int64]time.Time
	lastLogin map[int64]time.Time
	revealed  map[int64]bool
}

func loadFacts(ctx context.Context, d *sql.DB, groupID int64) (*facts, error) {
	f := &facts{mail: map[int64]mailState{}, lastSent: map[int64]time.Time{}, lastLogin: map[int64]time.Time{}, revealed: map[int64]bool{}}
	fail := func(err error) (*facts, error) { return nil, errs.Internal(err) }

	rows, err := d.QueryContext(ctx, `SELECT participant_id, status FROM mail_queue
		WHERE id IN (SELECT MAX(id) FROM mail_queue WHERE group_id = ? AND participant_id <> 0 GROUP BY participant_id)`, groupID)
	if err != nil {
		return fail(err)
	}
	for rows.Next() {
		var id int64
		var st string
		if err := rows.Scan(&id, &st); err != nil {
			_ = rows.Close()
			return fail(err)
		}
		switch st {
		case "sent":
			f.mail[id] = mailSent
		case "failed":
			f.mail[id] = mailFailed
		default:
			f.mail[id] = mailPending
		}
	}
	if err := rows.Close(); err != nil {
		return fail(err)
	}

	rows, err = d.QueryContext(ctx, `SELECT participant_id, MAX(sent_at) FROM mail_queue
		WHERE group_id = ? AND participant_id <> 0 AND status = 'sent' AND sent_at IS NOT NULL GROUP BY participant_id`, groupID)
	if err != nil {
		return fail(err)
	}
	for rows.Next() {
		var id, at int64
		if err := rows.Scan(&id, &at); err != nil {
			_ = rows.Close()
			return fail(err)
		}
		f.lastSent[id] = time.Unix(0, at).UTC()
	}
	if err := rows.Close(); err != nil {
		return fail(err)
	}

	rows, err = d.QueryContext(ctx, `SELECT CAST(subject AS INTEGER), MAX(created_at) FROM websec_sessions
		WHERE kind = 'participant' AND group_id = ? GROUP BY subject`, strconv.FormatInt(groupID, 10))
	if err != nil {
		return fail(err)
	}
	for rows.Next() {
		var id, at int64
		if err := rows.Scan(&id, &at); err != nil {
			_ = rows.Close()
			return fail(err)
		}
		f.lastLogin[id] = time.Unix(0, at).UTC()
	}
	if err := rows.Close(); err != nil {
		return fail(err)
	}

	rows, err = d.QueryContext(ctx, `SELECT p.giver_id FROM draw_pairs p JOIN draws d ON d.id = p.draw_id
		WHERE d.group_id = ? AND d.status = 'actuel' GROUP BY p.giver_id HAVING COUNT(p.discovered_at) = COUNT(*)`, groupID)
	if err != nil {
		return fail(err)
	}
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			_ = rows.Close()
			return fail(err)
		}
		f.revealed[id] = true
	}
	if err := rows.Close(); err != nil {
		return fail(err)
	}
	return f, nil
}

func (f *facts) state(id int64) mailState {
	if s, ok := f.mail[id]; ok {
		return s
	}
	return mailNone
}

type household struct {
	ID      int64
	Name    string
	Members []int64
}

func loadHouseholds(ctx context.Context, d *sql.DB, groupID int64) ([]household, error) {
	rows, err := d.QueryContext(ctx, `SELECT h.id, h.name, COALESCE(m.participant_id, 0) FROM households h
		LEFT JOIN household_members m ON m.household_id = h.id WHERE h.group_id = ? ORDER BY h.id, m.participant_id`, groupID)
	if err != nil {
		return nil, errs.Internal(err)
	}
	defer rows.Close()
	var out []household
	for rows.Next() {
		var id, pid int64
		var name string
		if err := rows.Scan(&id, &name, &pid); err != nil {
			return nil, errs.Internal(err)
		}
		if len(out) == 0 || out[len(out)-1].ID != id {
			out = append(out, household{ID: id, Name: name})
		}
		if pid != 0 {
			out[len(out)-1].Members = append(out[len(out)-1].Members, pid)
		}
	}
	return out, rows.Err()
}

type pair struct{ A, B int64 }

// loadExclusions lit les exclusions manuelles ; both est vrai quand les deux
// sens existent (la paire est alors rendue une fois, A < B).
type exclusion struct {
	A, B int64
	Both bool
}

func loadExclusions(ctx context.Context, d *sql.DB, groupID int64) ([]exclusion, int, error) {
	rows, err := d.QueryContext(ctx, `SELECT giver_id, receiver_id FROM manual_exclusions WHERE group_id = ?`, groupID)
	if err != nil {
		return nil, 0, errs.Internal(err)
	}
	set := map[pair]bool{}
	for rows.Next() {
		var a, b int64
		if err := rows.Scan(&a, &b); err != nil {
			_ = rows.Close()
			return nil, 0, errs.Internal(err)
		}
		set[pair{a, b}] = true
	}
	if err := rows.Close(); err != nil {
		return nil, 0, errs.Internal(err)
	}
	var out []exclusion
	for k := range set {
		if set[pair{k.B, k.A}] {
			if k.A < k.B {
				out = append(out, exclusion{A: k.A, B: k.B, Both: true})
			}
			continue
		}
		out = append(out, exclusion{A: k.A, B: k.B})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].A != out[j].A {
			return out[i].A < out[j].A
		}
		return out[i].B < out[j].B
	})
	var forced int
	if err := d.QueryRowContext(ctx, `SELECT COUNT(*) FROM forced_pairs WHERE group_id = ?`, groupID).Scan(&forced); err != nil {
		return nil, 0, errs.Internal(err)
	}
	return out, forced, nil
}

func errsKind(err error) string {
	switch {
	case errs.Is(err, errs.KindConflict):
		return "conflict"
	case errs.Is(err, errs.KindValidation):
		return "validation"
	}
	return ""
}
