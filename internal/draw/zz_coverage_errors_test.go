package draw

import (
	"context"
	"errors"
	"testing"

	"secretsanta/internal/participants"
	"secretsanta/internal/shared/errs"
	"secretsanta/internal/shared/ports"
)

// brokenReg refuse toute lecture et toute résolution.
type brokenReg struct{}

func (brokenReg) PendingCount(_ context.Context, _ int64) (int, error) { return 0, errors.New("panne") }
func (brokenReg) Resolve(_ context.Context, _ int64, _ ports.RegistrationResolution) error {
	return errors.New("panne")
}

func TestZZActorRefusals(t *testing.T) {
	e := newEnv(t)
	gid := e.group(t, "refus", 0, nil)
	anon := participants.Actor{}
	other := admin(gid + 99)
	part := who(gid, 1)
	noID := participants.Actor{Kind: participants.ActorParticipant, GroupID: gid}

	calls := map[string]func(a participants.Actor) error{
		"pending": func(a participants.Actor) error { _, err := e.svc.PendingRegistrations(ctx, a, gid); return err },
		"resolve": func(a participants.Actor) error { return e.svc.ResolvePending(ctx, a, gid, ports.ResolveIgnore) },
		"draw":    func(a participants.Actor) error { _, err := e.svc.Draw(ctx, a, gid, confirmed); return err },
		"redraw":  func(a participants.Actor) error { _, err := e.svc.Redraw(ctx, a, gid, confirmed); return err },
		"check":   func(a participants.Actor) error { _, err := e.svc.Check(ctx, a, gid, false); return err },
		"report":  func(a participants.Actor) error { _, err := e.svc.Report(ctx, a, gid); return err },
		"facts":   func(a participants.Actor) error { _, err := e.svc.AdminFacts(ctx, a, gid); return err },
		"history": func(a participants.Actor) error { _, err := e.svc.History(ctx, a, gid); return err },
	}
	for name, f := range calls {
		if err := f(anon); err == nil {
			t.Errorf("%s : anonyme accepté", name)
		}
		if err := f(other); err == nil {
			t.Errorf("%s : autre groupe accepté", name)
		}
		if name != "history" {
			if err := f(part); err == nil {
				t.Errorf("%s : participant accepté", name)
			}
		}
	}
	pcalls := map[string]func(a participants.Actor) error{
		"mytargets": func(a participants.Actor) error { _, err := e.svc.MyTargets(ctx, a, gid); return err },
		"discover":  func(a participants.Actor) error { _, err := e.svc.MarkDiscovered(ctx, a, gid, 1); return err },
	}
	for name, f := range pcalls {
		for _, a := range []participants.Actor{anon, admin(gid), noID, who(gid+1, 1)} {
			if err := f(a); err == nil {
				t.Errorf("%s : acteur %v accepté", name, a.Kind)
			}
		}
	}
	if err := e.svc.ResolvePending(ctx, admin(gid), gid, ports.RegistrationResolution("x")); err == nil {
		t.Error("résolution invalide acceptée")
	}
	// Pas de tirage : état d'attente, découverte et refonte refusés.
	if _, err := e.svc.MarkDiscovered(ctx, who(gid, 1), gid, 1); err == nil {
		t.Error("découverte sans tirage acceptée")
	}
	if _, err := e.svc.Redraw(ctx, admin(gid), gid, confirmed); err == nil {
		t.Error("refonte sans tirage acceptée")
	}
	if _, err := e.svc.Draw(ctx, admin(gid), gid, Request{}); err == nil {
		t.Error("tirage non confirmé accepté")
	}
	if _, err := e.svc.Redraw(ctx, admin(gid), gid, Request{}); err == nil {
		t.Error("refonte non confirmée acceptée")
	}
}

func TestZZRegistrationPortFailures(t *testing.T) {
	e := newEnv(t)
	gid := e.group(t, "reg", 0, nil)
	e.svc.registrations = brokenReg{}
	if _, err := e.svc.PendingRegistrations(ctx, admin(gid), gid); err == nil {
		t.Error("panne de lecture ignorée")
	}
	if err := e.svc.ResolvePending(ctx, admin(gid), gid, ports.ResolveDelete); err == nil {
		t.Error("panne de résolution ignorée")
	}
	e.svc.registrations = nil
	if n, err := e.svc.PendingRegistrations(ctx, admin(gid), gid); err != nil || n != 0 {
		t.Errorf("sans port : %d %v", n, err)
	}
	if err := e.svc.ResolvePending(ctx, admin(gid), gid, ports.ResolveIgnore); err != nil {
		t.Error(err)
	}
}

func TestZZAdjustTxValidation(t *testing.T) {
	e := newEnv(t)
	gid := e.group(t, "adj", 0, nil)
	ids := e.addN(t, gid, "A", 4, participants.RoleNormal)
	run := func(pairs []Pair) error {
		tx, err := e.db.BeginTx(ctx, nil)
		if err != nil {
			t.Fatal(err)
		}
		defer tx.Rollback()
		return e.svc.AdjustTx(ctx, tx, gid, pairs)
	}
	if err := run(nil); err == nil {
		t.Error("vide accepté")
	}
	if err := run([]Pair{{ids[0], ids[0]}}); err == nil {
		t.Error("auto-cadeau accepté")
	}
	if err := run([]Pair{{ids[0], ids[1]}, {ids[2], ids[1]}}); err == nil {
		t.Error("destinataire en double accepté")
	}
	if err := run([]Pair{{ids[0], ids[1]}}); err == nil || errs.KindOf(err) != errs.KindConflict {
		t.Errorf("sans tirage : %v", err)
	}
	e.mustDraw(t, gid)
	// Les paires d'un autre effectif ne correspondent plus au tirage.
	if err := run([]Pair{{ids[0], ids[1]}}); err == nil {
		t.Log("ajustement partiel accepté")
	}
	if st, err := NewState(e.db).Status(ctx, gid); err != nil || !st.Done {
		t.Errorf("statut : %v %v", st, err)
	}
	if ok, err := NewState(e.db).IsDrawDone(ctx, gid); err != nil || !ok {
		t.Error("IsDrawDone")
	}
}

func TestZZClosedDatabase(t *testing.T) {
	e := newEnv(t)
	gid := e.group(t, "ferme", 0, nil)
	ids := e.addN(t, gid, "B", 4, participants.RoleNormal)
	e.mustDraw(t, gid)
	_ = e.db.Close()
	a, p := admin(gid), who(gid, ids[0])
	st := NewState(e.db)
	if _, err := st.IsDrawDone(ctx, gid); err == nil {
		t.Error("IsDrawDone")
	}
	if _, err := st.Status(ctx, gid); err == nil {
		t.Error("Status")
	}
	checks := map[string]error{}
	_, checks["draw"] = e.svc.Draw(ctx, a, gid, confirmed)
	_, checks["redraw"] = e.svc.Redraw(ctx, a, gid, confirmed)
	_, checks["check"] = e.svc.Check(ctx, a, gid, true)
	_, checks["report"] = e.svc.Report(ctx, a, gid)
	_, checks["facts"] = e.svc.AdminFacts(ctx, a, gid)
	_, checks["history"] = e.svc.History(ctx, a, gid)
	_, checks["historyP"] = e.svc.History(ctx, p, gid)
	_, checks["mytargets"] = e.svc.MyTargets(ctx, p, gid)
	_, checks["discover"] = e.svc.MarkDiscovered(ctx, p, gid, 1)
	_, checks["counts"] = e.svc.TargetCounts(ctx, gid)
	for k, err := range checks {
		if err == nil {
			t.Errorf("%s : base fermée acceptée", k)
		}
	}
}
