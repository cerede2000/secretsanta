package draw

import (
	"strings"
	"testing"

	"secretsanta/internal/drawengine"
	"secretsanta/internal/participants"
	"secretsanta/internal/shared/errs"
)

func TestCapacityGateReadsRolesAndRefusesWithoutNames(t *testing.T) {
	e := newEnv(t)
	gid := e.group(t, "capacite-un", 0, nil)
	e.addN(t, gid, "Hortensia", 2, participants.RoleReceptionSeule)
	single := e.addN(t, gid, "Quentrix", 2, participants.RoleUnSeul)

	_, err := e.svc.Draw(ctx, admin(gid), gid, confirmed)
	msg := e.wantKind(t, err, errs.KindConflict)
	for _, want := range []string{"il manque 2 cadeaux", "J'offre plus", "normal", "plafond"} {
		if !strings.Contains(msg, want) {
			t.Fatalf("explication ou proposition absente (%q) : %q", want, msg)
		}
	}
	code, params := errs.CodeOf(err)
	if code != CodeInfeasible || params["missing"] != 2 {
		t.Fatalf("code %q %v", code, params)
	}
	if n := e.count(t, `SELECT COUNT(*) FROM draws`); n != 0 {
		t.Fatal("un tirage refusé a été écrit")
	}

	rep, err := e.svc.Check(ctx, admin(gid), gid, false)
	e.wantKind(t, err, errs.KindConflict)
	if rep == nil || rep.Green || rep.Capacity.ExtraGiftsNeeded != 2 || rep.Capacity.TotalCapacity != 0 {
		t.Fatalf("rapport de capacité inattendu : %+v", rep)
	}

	// Les rôles de F3 sont relus : passer les deux en « J'offre plus » suffit.
	for _, id := range single {
		if err := e.parts.SetRole(ctx, participants.AdminActor(gid), gid, id, participants.RoleOffrePlus); err != nil {
			t.Fatal(err)
		}
	}
	rep, err = e.svc.Check(ctx, admin(gid), gid, false)
	if err != nil || !rep.Green || rep.Capacity.OffrePlusCapacity != 2 {
		t.Fatalf("contrôle après changement de rôles : %+v %v", rep, err)
	}
	o := e.mustDraw(t, gid)
	if o.Report.GiversTwoGifts != 2 || !o.Report.Green {
		t.Fatalf("rapport du tirage : %+v", o.Report)
	}
}

func TestCapacityGateReadsGroupCapAndRequiresConfirmationAboveTwo(t *testing.T) {
	e := newEnv(t)
	gid := e.group(t, "capacite-deux", 3, nil)
	e.addN(t, gid, "Hortensia", 4, participants.RoleReceptionSeule)
	e.addN(t, gid, "Gaspardin", 2, participants.RoleOffrePlus)

	_, err := e.svc.Draw(ctx, admin(gid), gid, confirmed)
	e.wantKind(t, err, errs.KindValidation)
	code, params := errs.CodeOf(err)
	if code != CodeInfeasible || params["cause"] != string(drawengine.CauseCapNeedsConfirmation) {
		t.Fatalf("confirmation du plafond attendue : %q %v", code, params)
	}
	if n := e.count(t, `SELECT COUNT(*) FROM draws`); n != 0 {
		t.Fatal("un tirage non confirmé a été écrit")
	}

	o, err := e.svc.Draw(ctx, admin(gid), gid, Request{Confirmed: true, CapConfirmed: true})
	if err != nil {
		t.Fatalf("tirage confirmé refusé : %v", err)
	}
	if o.Report.GiversMoreGifts != 2 || o.Report.Capacity.Cap != 3 || !o.Report.Green {
		t.Fatalf("le plafond du groupe (3) n'est pas appliqué : %+v", o.Report)
	}

	// Même liste avec le plafond par défaut : capacité insuffisante.
	low := e.group(t, "capacite-trois", 2, nil)
	e.addN(t, low, "Hortensia", 4, participants.RoleReceptionSeule)
	e.addN(t, low, "Gaspardin", 2, participants.RoleOffrePlus)
	_, err = e.svc.Draw(ctx, admin(low), low, Request{Confirmed: true, CapConfirmed: true})
	msg := e.wantKind(t, err, errs.KindConflict)
	if !strings.Contains(msg, "il manque 2 cadeaux") {
		t.Fatalf("message : %q", msg)
	}
}
