package reminders

import (
	"context"
	"time"

	"secretsanta/internal/shared/errs"
)

// autoKinds sont les relances automatiques ici ; le rappel au tireur est
// livré par F7b.
var autoKinds = []Kind{KindLink, KindLetter}

// TickResult compte ce qu'un passage du planificateur a fait.
type TickResult struct {
	Queued int
}

// Tick est un passage du planificateur : l'orchestrateur l'appelle
// régulièrement avec l'horloge de F1. Il est sans état caché (le journal et
// les faits sont en base), donc sûr après un redémarrage : une relance n'est
// envoyée que si elle est due, dans la fenêtre d'envoi, que la personne n'a
// pas agi (vérifié à l'instant de l'envoi, même si elle était planifiée avant),
// que le plafond n'est pas atteint et qu'aucun courriel de relance n'est déjà
// parti aujourd'hui. Les personnes sans courriel ne sont jamais relancées ici.
func (e *Engine) Tick(ctx context.Context) (TickResult, error) {
	var res TickResult
	now := e.o.Clock.Now()
	if !e.o.Rules.InWindow(now) {
		return res, nil
	}
	ids, err := e.o.Directory.GroupIDs(ctx)
	if err != nil {
		return res, err
	}
	for _, gid := range ids {
		n, err := e.tickGroup(ctx, gid, now)
		res.Queued += n
		if err != nil {
			return res, err
		}
	}
	return res, nil
}

func (e *Engine) tickGroup(ctx context.Context, gid int64, now time.Time) (int, error) {
	st, err := e.o.Rules.Settings(ctx, gid)
	if err != nil || !st.Enabled {
		return 0, err
	}
	g, err := e.o.Directory.Group(ctx, gid)
	if err != nil {
		return 0, err
	}
	people, err := e.o.Directory.Participants(ctx, gid)
	if err != nil {
		return 0, err
	}
	queued := 0
	for _, p := range people {
		if p.Email == "" {
			continue // jamais relancé automatiquement : liste de remise manuelle
		}
		for _, kind := range autoKinds {
			count, last, err := e.history(ctx, gid, p.ID, kind)
			if err != nil {
				return queued, err
			}
			due, ok := e.o.Rules.DueAt(st, p.JoinedAt, count)
			if !ok || now.Before(due) {
				continue
			}
			if count > 0 && e.o.Rules.Day(now) < e.o.Rules.Day(e.spacingEnd(last, st.SpacingDays)) {
				continue
			}
			late, err := e.late(ctx, gid, p, kind, nil)
			if err != nil {
				return queued, errs.Internal(err)
			}
			if !late {
				continue
			}
			out, err := e.send(ctx, g, p, kind, true)
			if err != nil {
				return queued, err
			}
			if out == OutcomeQueued {
				queued++
				break // un seul courriel par personne et par jour
			}
		}
	}
	return queued, nil
}

// history donne le nombre de relances automatiques déjà envoyées pour ce type
// et l'instant de la dernière.
func (e *Engine) history(ctx context.Context, gid, pid int64, kind Kind) (int, time.Time, error) {
	var n int
	var last *string
	err := e.o.DB.QueryRowContext(ctx, `SELECT COUNT(*), MAX(sent_at) FROM reminders_sent
		WHERE group_id = ? AND participant_id = ? AND kind = ? AND automatic = 1`, gid, pid, string(kind)).Scan(&n, &last)
	if err != nil {
		return 0, time.Time{}, errs.Internal(err)
	}
	var t time.Time
	if last != nil {
		if t, err = time.Parse(time.RFC3339, *last); err != nil {
			return 0, time.Time{}, errs.Internal(err)
		}
	}
	return n, t, nil
}

// spacingEnd renvoie l'instant « last + jours » en jours civils locaux.
func (e *Engine) spacingEnd(last time.Time, days int) time.Time {
	l := last.In(e.o.Rules.Location())
	y, m, d := l.Date()
	return time.Date(y, m, d+days, 12, 0, 0, 0, e.o.Rules.Location())
}
