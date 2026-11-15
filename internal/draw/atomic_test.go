package draw

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"secretsanta/internal/participants"
	"secretsanta/internal/shared/errs"
)

// failMidway installe un déclencheur qui fait échouer l'insertion de la
// troisième paire d'un tirage : l'erreur survient au milieu de l'écriture.
func failMidway(t *testing.T, e *env) {
	t.Helper()
	if _, err := e.db.Exec(`CREATE TRIGGER test_fail_midway BEFORE INSERT ON draw_pairs
		WHEN (SELECT COUNT(*) FROM draw_pairs WHERE draw_id = NEW.draw_id) >= 2
		BEGIN SELECT RAISE(ABORT, 'panne simulee au milieu'); END`); err != nil {
		t.Fatal(err)
	}
}

func dropFail(t *testing.T, e *env) {
	t.Helper()
	if _, err := e.db.Exec(`DROP TRIGGER test_fail_midway`); err != nil {
		t.Fatal(err)
	}
}

type snapshot struct {
	Draws  []string
	Pairs  []rawPair
	Events int
}

func (e *env) snapshot(t *testing.T, gid int64) snapshot {
	t.Helper()
	var s snapshot
	rows, err := e.db.Query(`SELECT id || '|' || year || '|' || kind || '|' || status || '|' || revision || '|' ||
		drawn_at || '|' || COALESCE(archived_at, '') FROM draws WHERE group_id = ? ORDER BY id`, gid)
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var line string
		if err := rows.Scan(&line); err != nil {
			t.Fatal(err)
		}
		s.Draws = append(s.Draws, line)
	}
	rows.Close()
	prs, err := e.db.Query(`SELECT p.id, p.giver_id, p.receiver_id, p.discovered_at FROM draw_pairs p
		JOIN draws d ON d.id = p.draw_id WHERE d.group_id = ? ORDER BY p.id`, gid)
	if err != nil {
		t.Fatal(err)
	}
	for prs.Next() {
		var p rawPair
		if err := prs.Scan(&p.ID, &p.Giver, &p.Receiver, &p.Discovered); err != nil {
			t.Fatal(err)
		}
		s.Pairs = append(s.Pairs, p)
	}
	prs.Close()
	s.Events = e.count(t, `SELECT COUNT(*) FROM draw_events WHERE group_id = ?`, gid)
	return s
}

func TestAtomicFirstDrawFailureWritesNothing(t *testing.T) {
	e := newEnv(t)
	gid := e.group(t, "atome-un", 0, nil)
	e.addN(t, gid, "Xavarin", 5, participants.RoleNormal)
	failMidway(t, e)

	_, err := e.svc.Draw(ctx, admin(gid), gid, confirmed)
	msg := e.wantKind(t, err, errs.KindInternal)
	if strings.Contains(msg, "panne") {
		t.Fatalf("détail technique divulgué : %q", msg)
	}
	if n := e.count(t, `SELECT COUNT(*) FROM draws`); n != 0 {
		t.Fatalf("%d tirage(s) écrit(s) malgré l'erreur", n)
	}
	if n := e.count(t, `SELECT COUNT(*) FROM draw_pairs`); n != 0 {
		t.Fatalf("%d paire(s) écrite(s) malgré l'erreur", n)
	}
	if n := e.count(t, `SELECT COUNT(*) FROM draw_events`); n != 0 {
		t.Fatalf("%d événement(s) écrit(s) malgré l'erreur", n)
	}
	if done, _ := e.svc.IsDrawDone(ctx, gid); done {
		t.Fatal("le tirage apparaît fait après un échec")
	}

	// Une fois la panne levée, le tirage passe.
	dropFail(t, e)
	o := e.mustDraw(t, gid)
	if got := len(e.rawPairs(t, o.DrawID)); got != 5 {
		t.Fatalf("%d paires, 5 attendues", got)
	}
}

func TestAtomicRedrawFailureKeepsPreviousDraw(t *testing.T) {
	e := newEnv(t)
	gid := e.group(t, "atome-deux", 0, nil)
	ids := e.addN(t, gid, "Ombeline", 6, participants.RoleNormal)
	first := e.mustDraw(t, gid)
	mine, err := e.svc.MyTargets(ctx, who(gid, ids[0]), gid)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.svc.MarkDiscovered(ctx, who(gid, ids[0]), gid, mine.Targets[0].PairID); err != nil {
		t.Fatal(err)
	}
	before := e.snapshot(t, gid)

	failMidway(t, e)
	_, err = e.svc.Redraw(ctx, admin(gid), gid, confirmed)
	e.wantKind(t, err, errs.KindInternal)

	after := e.snapshot(t, gid)
	if !reflect.DeepEqual(before, after) {
		t.Fatalf("le tirage précédent a été altéré :\navant %+v\naprès %+v", before, after)
	}
	st, err := e.svc.Status(ctx, gid)
	if err != nil {
		t.Fatal(err)
	}
	if !st.Done || st.DrawID != first.DrawID {
		t.Fatalf("le tirage actuel devrait rester %d : %+v", first.DrawID, st)
	}
}

// changingRoster ajoute un participant juste après avoir lu la liste : la
// liste a changé avant l'écriture.
type changingRoster struct {
	inner RosterSource
	after func()
}

func (c changingRoster) Roster(ctx context.Context, gid int64) (*participants.Roster, error) {
	r, err := c.inner.Roster(ctx, gid)
	c.after()
	return r, err
}

func TestAtomicRosterChangeDuringDrawWritesNothing(t *testing.T) {
	e := newEnv(t)
	gid := e.group(t, "atome-trois", 0, nil)
	e.addN(t, gid, "Quentrix", 4, participants.RoleNormal)
	added := false
	e.svc.roster = changingRoster{inner: e.parts, after: func() {
		if !added {
			added = true
			e.add(t, gid, "Tardivel", participants.RoleNormal)
		}
	}}
	_, err := e.svc.Draw(ctx, admin(gid), gid, confirmed)
	e.wantKind(t, err, errs.KindConflict)
	if n := e.count(t, `SELECT COUNT(*) FROM draw_pairs`); n != 0 {
		t.Fatalf("%d paire(s) écrite(s) avec une liste périmée", n)
	}
	if n := e.count(t, `SELECT COUNT(*) FROM draws`); n != 0 {
		t.Fatalf("%d tirage(s) écrit(s) avec une liste périmée", n)
	}
}

func TestDrawStoredRowsAreGuarded(t *testing.T) {
	e := newEnv(t)
	gid := e.group(t, "atome-quatre", 0, nil)
	e.addN(t, gid, "Gaspardin", 4, participants.RoleNormal)
	o := e.mustDraw(t, gid)
	p := e.rawPairs(t, o.DrawID)[0]
	if _, err := e.db.Exec(`UPDATE draw_pairs SET receiver_id = giver_id + 1000 WHERE id = ?`, p.ID); err == nil {
		t.Fatal("une paire a pu être réécrite en place")
	}
	if _, err := e.db.Exec(`UPDATE draw_pairs SET discovered_at = '2026-01-01T00:00:00Z' WHERE id = ?`, p.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := e.db.Exec(`UPDATE draw_pairs SET discovered_at = NULL WHERE id = ?`, p.ID); err == nil {
		t.Fatal("une découverte a pu revenir en arrière")
	}
	if _, err := e.db.Exec(`INSERT INTO draws (group_id, year, kind, status, gift_cap, drawn_at) VALUES (?, 2026, 'tirage', 'actuel', 2, 'x')`, gid); err == nil {
		t.Fatal("deux tirages actuels ont pu coexister")
	}
}
