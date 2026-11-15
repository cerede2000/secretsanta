package draw

import (
	"strings"
	"testing"

	"secretsanta/internal/participants"
	"secretsanta/internal/shared/errs"
	"secretsanta/internal/shared/ports"
)

func TestPendingRegistrationsBlockDrawWithCount(t *testing.T) {
	for _, how := range []ports.RegistrationResolution{ports.ResolveIgnore, ports.ResolveDelete} {
		t.Run(string(how), func(t *testing.T) {
			e := newEnv(t)
			gid := e.group(t, "attente-"+string(how), 0, nil)
			e.addN(t, gid, "Xavarin", 4, participants.RoleNormal)
			e.reg.Pending[gid] = 3

			n, err := e.svc.PendingRegistrations(ctx, admin(gid), gid)
			if err != nil || n != 3 {
				t.Fatalf("PendingRegistrations = %d, %v ; 3 attendu", n, err)
			}
			_, err = e.svc.Draw(ctx, admin(gid), gid, confirmed)
			msg := e.wantKind(t, err, errs.KindConflict)
			if !strings.Contains(msg, "3 inscriptions en attente") {
				t.Fatalf("le message doit donner le nombre : %q", msg)
			}
			code, params := errs.CodeOf(err)
			if code != CodePendingRegistrations || params["count"] != 3 {
				t.Fatalf("code %q %v", code, params)
			}
			if done, _ := e.svc.IsDrawDone(ctx, gid); done {
				t.Fatal("le tirage ne doit pas être fait")
			}
			if n := e.count(t, `SELECT COUNT(*) FROM draws`); n != 0 {
				t.Fatalf("%d tirage(s) écrit(s)", n)
			}

			if err := e.svc.ResolvePending(ctx, admin(gid), gid, how); err != nil {
				t.Fatal(err)
			}
			if e.reg.Resolved[gid] != how {
				t.Fatalf("choix transmis %q, attendu %q", e.reg.Resolved[gid], how)
			}
			e.mustDraw(t, gid)
			if done, _ := e.svc.IsDrawDone(ctx, gid); !done {
				t.Fatal("le tirage doit être fait après le choix explicite")
			}
		})
	}
}

func TestPendingRegistrationSingularAndResolutionRules(t *testing.T) {
	e := newEnv(t)
	gid := e.group(t, "attente-une", 0, nil)
	e.addN(t, gid, "Ombeline", 4, participants.RoleNormal)
	e.reg.Pending[gid] = 1
	_, err := e.svc.Draw(ctx, admin(gid), gid, confirmed)
	msg := e.wantKind(t, err, errs.KindConflict)
	if !strings.Contains(msg, "1 inscription en attente n'a pas été traitée") {
		t.Fatalf("accord au singulier attendu : %q", msg)
	}
	e.wantKind(t, e.svc.ResolvePending(ctx, admin(gid), gid, "oublier"), errs.KindValidation)
	if _, ok := e.reg.Resolved[gid]; ok {
		t.Fatal("un choix invalide ne doit rien résoudre")
	}
	e.wantKind(t, e.svc.ResolvePending(ctx, who(gid, 1), gid, ports.ResolveDelete), errs.KindForbidden)
	if _, ok := e.reg.Resolved[gid]; ok {
		t.Fatal("seul l'administrateur résout les inscriptions")
	}
}

func TestNoPendingRegistrationDrawsDirectly(t *testing.T) {
	e := newEnv(t)
	gid := e.group(t, "attente-aucune", 0, nil)
	e.addN(t, gid, "Quentrix", 4, participants.RoleNormal)
	e.mustDraw(t, gid)
	if len(e.reg.Resolved) != 0 {
		t.Fatal("aucune résolution ne doit être faite sans inscription en attente")
	}
}

func TestPendingRegistrationsBlockRedraw(t *testing.T) {
	e := newEnv(t)
	gid := e.group(t, "attente-refait", 0, nil)
	e.addN(t, gid, "Gaspardin", 4, participants.RoleNormal)
	first := e.mustDraw(t, gid)
	e.reg.Pending[gid] = 2
	_, err := e.svc.Redraw(ctx, admin(gid), gid, confirmed)
	msg := e.wantKind(t, err, errs.KindConflict)
	if !strings.Contains(msg, "2 inscriptions") {
		t.Fatalf("message : %q", msg)
	}
	st, _ := e.svc.Status(ctx, gid)
	if st.DrawID != first.DrawID {
		t.Fatal("le tirage actuel ne doit pas changer")
	}
}
