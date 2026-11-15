package dashboard

import (
	"strings"
	"testing"

	"secretsanta/internal/participants"
)

// F10.AC5 : le compteur d'inscriptions par lien en attente, son lien vers la
// page d'administration des inscriptions, et aucun nom de cible.
func TestDashboardShowsPendingRegistrationsCounter(t *testing.T) {
	e := newEnv(t)
	gid := e.group("attente")
	e.addN(gid, "Hortensia", 4, participants.RoleNormal)
	e.reg.Pending[gid] = 3
	e.doDraw2(t, gid)

	v, err := e.svc.Dashboard(ctx, admin(gid), gid)
	if err != nil {
		t.Fatal(err)
	}
	if v.PendingRegistrations != 3 {
		t.Fatalf("compteur : %d", v.PendingRegistrations)
	}
	ck := e.adminCookie(gid)
	for lang, want := range map[string]string{"fr": "3 inscriptions en attente", "en": "3 sign-ups waiting"} {
		body := e.get("/attente/admin", lang, ck).Body.String()
		if !strings.Contains(body, want) {
			t.Errorf("%s : %q absent", lang, want)
		}
		if !strings.Contains(body, `href="/attente/admin/inscriptions"`) {
			t.Errorf("%s : lien vers la page des inscriptions absent", lang)
		}
		for _, pr := range e.pairs(gid) {
			row := rowOf(t, body, pr[0])
			_ = row
		}
	}
	// Un seul : singulier. Aucun : message neutre.
	e.reg.Pending[gid] = 1
	if body := e.get("/attente/admin", "fr", ck).Body.String(); !strings.Contains(body, "1 inscription en attente") {
		t.Error("singulier absent")
	}
	delete(e.reg.Pending, gid)
	body := e.get("/attente/admin", "fr", ck).Body.String()
	if !strings.Contains(body, "Aucune inscription en attente") {
		t.Error("message sans inscription absent")
	}
}

// doDraw2 tire sans que les inscriptions en attente bloquent le test : le
// tirage lui-même exige leur résolution (F4), pas le tableau de bord.
func (e *env) doDraw2(t *testing.T, gid int64) {
	t.Helper()
	pending := e.reg.Pending[gid]
	delete(e.reg.Pending, gid)
	e.doDraw(gid)
	e.reg.Pending[gid] = pending
}

func TestPendingCounterIsZeroWithoutPort(t *testing.T) {
	e := newEnv(t)
	gid := e.group("sans-port")
	e.addN(gid, "Hortensia", 2, participants.RoleNormal)
	svc, err := New(Options{DB: e.db, Clock: e.clock, Participants: e.parts, Draws: e.draws, Constraints: e.rules, Audit: e.audit})
	if err != nil {
		t.Fatal(err)
	}
	v, err := svc.Dashboard(ctx, admin(gid), gid)
	if err != nil || v.PendingRegistrations != 0 || v.RevealDone {
		t.Fatalf("sans port : %+v %v", v, err)
	}
}
