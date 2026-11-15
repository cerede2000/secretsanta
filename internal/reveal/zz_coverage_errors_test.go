package reveal

import (
	"errors"
	"testing"
	"time"

	"secretsanta/internal/participants"
)

func TestZZActorAndGroupRefusals(t *testing.T) {
	e := newEnv(t)
	gid := e.group(t, "refus", nil)
	at := t0.Add(48 * time.Hour)
	for _, a := range []participants.Actor{{}, who(gid, 1), admin(gid + 5)} {
		if err := e.svc.SetRevealDate(ctx, a, gid, &at); err == nil {
			t.Errorf("SetRevealDate accepté pour %v", a.Kind)
		}
		if _, err := e.svc.OpenNow(ctx, a, gid, true); err == nil {
			t.Errorf("OpenNow accepté pour %v", a.Kind)
		}
		if _, err := e.svc.Facts(ctx, a, gid); err == nil {
			t.Errorf("Facts accepté pour %v", a.Kind)
		}
	}
	if _, err := e.svc.OpenNow(ctx, admin(gid), gid, false); err == nil {
		t.Error("ouverture sans confirmation acceptée")
	}
	if _, err := e.svc.OpenNow(ctx, admin(gid), gid, true); err == nil {
		t.Error("ouverture sans tirage acceptée")
	}
	// Groupe inconnu.
	if err := e.svc.SetRevealDate(ctx, admin(777), 777, &at); err == nil {
		t.Error("date sur un groupe inconnu acceptée")
	}
	if err := e.svc.OnDrawChanged(ctx, 777); err == nil {
		t.Error("OnDrawChanged sur un groupe inconnu accepté")
	}
	if _, err := e.svc.Facts(ctx, admin(777), 777); err == nil {
		t.Error("Facts sur un groupe inconnu accepté")
	}
	if _, err := e.svc.OpenNow(ctx, admin(777), 777, true); err == nil {
		t.Error("OpenNow sur un groupe inconnu accepté")
	}
	// Date hors bornes.
	far := time.Date(12000, 1, 1, 0, 0, 0, 0, time.UTC)
	if err := e.svc.SetRevealDate(ctx, admin(gid), gid, &far); err == nil {
		t.Error("date lointaine acceptée")
	}
}

func TestZZRestoreReschedulesAndReportsMailFailure(t *testing.T) {
	e := newEnv(t)
	future := t0.Add(72 * time.Hour)
	dated := e.group(t, "datee", &future)
	e.addN(t, dated, "R", 3, participants.RoleNormal)
	undrawn := e.group(t, "sansdate", nil)
	e.addN(t, undrawn, "S", 3, participants.RoleNormal)
	e.mail.err = errors.New("file indisponible")
	e.mustDraw(t, undrawn) // ouvre aussitôt, l'avis échoue
	if err := e.svc.Restore(ctx); err == nil {
		t.Error("l'échec des avis doit remonter")
	}
	e.mail.err = nil
	if err := e.svc.Restore(ctx); err != nil {
		t.Errorf("reprise : %v", err)
	}
	if len(e.mail.notices()) == 0 {
		t.Error("avis non rattrapés")
	}
	// Un groupe daté sans tirage est replanifié.
	if e.sched.Pending() == 0 {
		t.Error("ouverture non replanifiée")
	}
}

func TestZZFireRetriesAfterFailure(t *testing.T) {
	e := newEnv(t)
	future := t0.Add(24 * time.Hour)
	gid := e.group(t, "reprise", &future)
	e.addN(t, gid, "F", 3, participants.RoleNormal)
	e.mustDraw(t, gid)
	e.clock.Advance(25 * time.Hour)
	if _, err := e.db.Exec(`ALTER TABLE reveal_openings RENAME TO reveal_openings_off`); err != nil {
		t.Fatal(err)
	}
	if _, err := e.sched.Tick(ctx); err == nil {
		t.Error("échec d'ouverture attendu")
	}
	if e.sched.Pending() == 0 {
		t.Error("reprise non planifiée")
	}
}

func TestZZClosedDatabase(t *testing.T) {
	e := newEnv(t)
	gid := e.group(t, "ferme", nil)
	e.addN(t, gid, "G", 3, participants.RoleNormal)
	e.mustDraw(t, gid)
	_ = e.db.Close()
	at := t0.Add(time.Hour)
	if err := e.svc.Restore(ctx); err == nil {
		t.Error("Restore")
	}
	if err := e.svc.Flush(ctx); err == nil {
		t.Error("Flush")
	}
	if err := e.svc.SetRevealDate(ctx, admin(gid), gid, &at); err == nil {
		t.Error("SetRevealDate")
	}
	if _, err := e.svc.OpenNow(ctx, admin(gid), gid, true); err == nil {
		t.Error("OpenNow")
	}
	if _, err := e.svc.Facts(ctx, admin(gid), gid); err == nil {
		t.Error("Facts")
	}
	if err := e.svc.OnDrawChanged(ctx, gid); err == nil {
		t.Error("OnDrawChanged")
	}
}
