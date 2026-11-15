package dashboard

import (
	"context"
	"database/sql"
	"strconv"

	"secretsanta/internal/shared/errs"
)

type mailInfo struct {
	last     MailState
	everSent bool
}

// activity regroupe, par personne, les faits d'activité du groupe. Aucune
// paire : « targetsSeen » est un booléen par tireur.
type activity struct {
	linkOpened    map[int64]bool
	handedOver    map[int64]bool
	targetsSeen   map[int64]bool
	letterWritten map[int64]bool
	letterRead    map[int64]bool
	mail          map[int64]mailInfo
}

func newActivity() activity {
	return activity{
		linkOpened: map[int64]bool{}, handedOver: map[int64]bool{}, targetsSeen: map[int64]bool{},
		letterWritten: map[int64]bool{}, letterRead: map[int64]bool{}, mail: map[int64]mailInfo{},
	}
}

func scanIDs(rows *sql.Rows, each func(id int64)) error {
	defer rows.Close()
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return err
		}
		each(id)
	}
	return rows.Err()
}

// loadActivity lit les faits d'activité du groupe dans les tables des autres
// modules (lecture seule).
func loadActivity(ctx context.Context, d *sql.DB, groupID int64) (activity, error) {
	act := newActivity()
	fail := func(err error) (activity, error) { return activity{}, errs.Internal(err) }

	// Lien ouvert : la personne a ouvert une session (lien, code ou lien magique).
	rows, err := d.QueryContext(ctx, `SELECT DISTINCT CAST(subject AS INTEGER) FROM websec_sessions
		WHERE kind = 'participant' AND group_id = ?`, strconv.FormatInt(groupID, 10))
	if err != nil {
		return fail(err)
	}
	if err := scanIDs(rows, func(id int64) { act.linkOpened[id] = true }); err != nil {
		return fail(err)
	}

	rows, err = d.QueryContext(ctx, `SELECT participant_id FROM admin_link_handover WHERE group_id = ?`, groupID)
	if err != nil {
		return fail(err)
	}
	if err := scanIDs(rows, func(id int64) { act.handedOver[id] = true }); err != nil {
		return fail(err)
	}

	// Cibles vues : le tireur a découvert toutes ses cibles (aucune n'est nommée).
	rows, err = d.QueryContext(ctx, `SELECT p.giver_id FROM draw_pairs p JOIN draws d ON d.id = p.draw_id
		WHERE d.group_id = ? AND d.status = 'actuel' GROUP BY p.giver_id HAVING COUNT(p.discovered_at) = COUNT(*)`, groupID)
	if err != nil {
		return fail(err)
	}
	if err := scanIDs(rows, func(id int64) { act.targetsSeen[id] = true }); err != nil {
		return fail(err)
	}

	rows, err = d.QueryContext(ctx, `SELECT participant_id FROM letters WHERE group_id = ? AND version > 0`, groupID)
	if err != nil {
		return fail(err)
	}
	if err := scanIDs(rows, func(id int64) { act.letterWritten[id] = true }); err != nil {
		return fail(err)
	}
	rows, err = d.QueryContext(ctx, `SELECT l.participant_id FROM letters l JOIN letter_reads r ON r.letter_id = l.id
		WHERE l.group_id = ?`, groupID)
	if err != nil {
		return fail(err)
	}
	if err := scanIDs(rows, func(id int64) { act.letterRead[id] = true }); err != nil {
		return fail(err)
	}

	// Courriels : état du dernier message, et au moins un envoi réussi.
	mrows, err := d.QueryContext(ctx, `SELECT q.participant_id, q.status,
			EXISTS (SELECT 1 FROM mail_queue s WHERE s.group_id = q.group_id AND s.participant_id = q.participant_id AND s.status = 'sent')
		FROM mail_queue q
		WHERE q.id IN (SELECT MAX(id) FROM mail_queue WHERE group_id = ? AND participant_id <> 0 GROUP BY participant_id)`, groupID)
	if err != nil {
		return fail(err)
	}
	defer mrows.Close()
	for mrows.Next() {
		var id int64
		var status string
		var ever bool
		if err := mrows.Scan(&id, &status, &ever); err != nil {
			return fail(err)
		}
		st := MailPending
		switch status {
		case "sent":
			st = MailSent
		case "failed":
			st = MailFailed
		}
		act.mail[id] = mailInfo{last: st, everSent: ever}
	}
	if err := mrows.Err(); err != nil {
		return fail(err)
	}
	return act, nil
}

// drawPair est une affectation lue pour le contrôle ; elle ne quitte jamais
// ce paquet.
type drawPair struct{ giver, receiver int64 }

// currentDraw lit le plafond et les affectations du tirage actuel ; ok est
// faux sans tirage.
func currentDraw(ctx context.Context, d *sql.DB, groupID int64) (capN int, pairs []drawPair, ok bool, err error) {
	var drawID int64
	e := d.QueryRowContext(ctx, `SELECT id, gift_cap FROM draws WHERE group_id = ? AND status = 'actuel'`, groupID).Scan(&drawID, &capN)
	if e == sql.ErrNoRows {
		return 0, nil, false, nil
	}
	if e != nil {
		return 0, nil, false, errs.Internal(e)
	}
	rows, e := d.QueryContext(ctx, `SELECT giver_id, receiver_id FROM draw_pairs WHERE draw_id = ?`, drawID)
	if e != nil {
		return 0, nil, false, errs.Internal(e)
	}
	defer rows.Close()
	for rows.Next() {
		var p drawPair
		if e := rows.Scan(&p.giver, &p.receiver); e != nil {
			return 0, nil, false, errs.Internal(e)
		}
		pairs = append(pairs, p)
	}
	if e := rows.Err(); e != nil {
		return 0, nil, false, errs.Internal(e)
	}
	return capN, pairs, true, nil
}
