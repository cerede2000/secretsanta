package draw

import (
	"testing"

	"secretsanta/internal/participants"
)

func (e *env) mustExec(t *testing.T, q string) {
	t.Helper()
	if _, err := e.db.Exec(q); err != nil {
		t.Fatalf("%s : %v", q, err)
	}
}

func TestZZPersistFailures(t *testing.T) {
	for name, trig := range map[string]string{
		"insert-draws":  `CREATE TRIGGER zz BEFORE INSERT ON draws BEGIN SELECT RAISE(ABORT, 'panne'); END`,
		"insert-events": `CREATE TRIGGER zz BEFORE INSERT ON draw_events BEGIN SELECT RAISE(ABORT, 'panne'); END`,
		"insert-pairs":  `CREATE TRIGGER zz BEFORE INSERT ON draw_pairs BEGIN SELECT RAISE(ABORT, 'panne'); END`,
	} {
		t.Run(name, func(t *testing.T) {
			e := newEnv(t)
			gid := e.group(t, "p"+name[len(name)-3:], 0, nil)
			e.addN(t, gid, "C", 4, participants.RoleNormal)
			e.mustExec(t, trig)
			if _, err := e.svc.Draw(ctx, admin(gid), gid, confirmed); err == nil {
				t.Fatal("panne ignorée")
			}
		})
	}
	for name, trig := range map[string]string{
		"update-draws":  `CREATE TRIGGER zz BEFORE UPDATE ON draws BEGIN SELECT RAISE(ABORT, 'panne'); END`,
		"insert-events": `CREATE TRIGGER zz BEFORE INSERT ON draw_events BEGIN SELECT RAISE(ABORT, 'panne'); END`,
	} {
		t.Run("redraw-"+name, func(t *testing.T) {
			e := newEnv(t)
			gid := e.group(t, "redraw", 0, nil)
			e.addN(t, gid, "D", 4, participants.RoleNormal)
			e.mustDraw(t, gid)
			e.mustExec(t, trig)
			if _, err := e.svc.Redraw(ctx, admin(gid), gid, confirmed); err == nil {
				t.Fatal("panne ignorée")
			}
		})
	}
}

func TestZZUnknownGroupAndSources(t *testing.T) {
	e := newEnv(t)
	if _, err := e.svc.Draw(ctx, admin(4242), 4242, confirmed); err == nil {
		t.Error("groupe inconnu accepté")
	}
	if _, err := e.svc.Check(ctx, admin(4242), 4242, false); err == nil {
		t.Error("contrôle d'un groupe inconnu accepté")
	}
}

func TestZZBrokenReadModel(t *testing.T) {
	setup := func(t *testing.T) (*env, int64, int64) {
		e := newEnv(t)
		gid := e.group(t, "lecture", 0, nil)
		ids := e.addN(t, gid, "E", 4, participants.RoleNormal)
		e.mustDraw(t, gid)
		return e, gid, ids[0]
	}
	t.Run("sans-paires", func(t *testing.T) {
		e, gid, pid := setup(t)
		e.mustExec(t, `DROP TRIGGER draw_pairs_immutable`)
		e.mustExec(t, `DROP TABLE draw_pairs`)
		for k, f := range map[string]func() error{
			"history":   func() error { _, err := e.svc.History(ctx, who(gid, pid), gid); return err },
			"historyA":  func() error { _, err := e.svc.History(ctx, admin(gid), gid); return err },
			"mytargets": func() error { _, err := e.svc.MyTargets(ctx, who(gid, pid), gid); return err },
			"report":    func() error { _, err := e.svc.Report(ctx, admin(gid), gid); return err },
			"facts":     func() error { _, err := e.svc.AdminFacts(ctx, admin(gid), gid); return err },
			"counts":    func() error { _, err := e.svc.TargetCounts(ctx, gid); return err },
			"discover":  func() error { _, err := e.svc.MarkDiscovered(ctx, who(gid, pid), gid, 1); return err },
		} {
			if f() == nil {
				t.Errorf("%s : table absente acceptée", k)
			}
		}
	})
	t.Run("sans-evenements", func(t *testing.T) {
		e, gid, pid := setup(t)
		e.mustExec(t, `DROP TABLE draw_events`)
		if _, err := e.svc.History(ctx, who(gid, pid), gid); err == nil {
			t.Error("historique sans évènements accepté")
		}
		if _, err := e.svc.Redraw(ctx, admin(gid), gid, confirmed); err == nil {
			t.Error("refonte sans évènements acceptée")
		}
	})
	t.Run("date-illisible", func(t *testing.T) {
		e, gid, pid := setup(t)
		e.mustExec(t, `DROP TRIGGER IF EXISTS draws_archive_final`)
		e.mustExec(t, `UPDATE draws SET drawn_at = 'pas une date'`)
		if _, err := e.svc.Draw(ctx, admin(gid), gid, confirmed); err == nil {
			t.Error("date illisible acceptée")
		}
		if _, err := NewState(e.db).IsDrawDone(ctx, gid); err == nil {
			t.Error("IsDrawDone")
		}
		if _, err := e.svc.MyTargets(ctx, who(gid, pid), gid); err == nil {
			t.Error("MyTargets")
		}
	})
	t.Run("evenement-date-illisible", func(t *testing.T) {
		e, gid, pid := setup(t)
		e.mustExec(t, `UPDATE draw_events SET at = 'pas une date'`)
		if _, err := e.svc.History(ctx, who(gid, pid), gid); err == nil {
			t.Error("date d'évènement illisible acceptée")
		}
	})
}
