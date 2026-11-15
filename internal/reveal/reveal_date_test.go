package reveal

import (
	"testing"
	"time"

	"secretsanta/internal/participants"
	"secretsanta/internal/shared/errs"
	"secretsanta/internal/shared/scheduler"
)

// AC6 : avant la date, la cible n'est pas renvoyée ; à la date, le courriel
// « tirage prêt » est mis en file avec l'horloge simulée ; sans date, la
// cible est immédiate.
func TestRevealDateGatesTargetsAndQueuesReadyMail(t *testing.T) {
	e := newEnv(t)
	at := t0.Add(48 * time.Hour)
	gid := e.group(t, "date-a-venir", &at)
	ids := e.addN(t, gid, "Ombeline", 4, participants.RoleNormal)
	o := e.mustDraw(t, gid)

	e.wantClosed(t, gid, ids)
	raw := e.rawPairs(t, o.DrawID)
	_, err := e.draw.MarkDiscovered(ctx, who(gid, ids[0]), gid, raw[0].ID)
	if err == nil {
		t.Fatal("une découverte a été acceptée avant la date")
	}
	e.noNames(t, err.Error())
	if n := len(e.mail.notices()); n != 0 {
		t.Fatalf("%d avis partis avant la date", n)
	}
	f, err := e.svc.Facts(ctx, admin(gid), gid)
	if err != nil {
		t.Fatal(err)
	}
	if !f.DrawDone || f.Open || f.OpenedAt != nil || f.DateLocked {
		t.Fatalf("faits avant la date : %+v", f)
	}

	// Une seconde avant la date : toujours fermé, aucune tâche exécutée.
	e.clock.Set(at.Add(-time.Second))
	if n := e.tick(t); n != 0 {
		t.Fatalf("%d tâche(s) exécutée(s) avant la date", n)
	}
	e.wantClosed(t, gid, ids)
	if n := len(e.mail.notices()); n != 0 {
		t.Fatalf("%d avis partis avant la date", n)
	}

	// À la date : la tâche ouvre et met en file un avis par personne.
	e.clock.Set(at)
	if n := e.tick(t); n != 1 {
		t.Fatalf("la tâche d'ouverture devait s'exécuter une fois, %d exécution(s)", n)
	}
	e.wantOpen(t, gid, o.DrawID, ids)
	sent := e.mail.notices()
	if len(sent) != len(ids) {
		t.Fatalf("%d avis « tirage prêt », %d attendus", len(sent), len(ids))
	}
	got := map[int64]int{}
	for _, n := range sent {
		if n.GroupID != gid || n.DrawID != o.DrawID || n.Targets != 1 || n.ReceptionOnly {
			t.Fatalf("avis inattendu : %+v", n)
		}
		got[n.ParticipantID]++
	}
	for _, id := range ids {
		if got[id] != 1 {
			t.Fatalf("participant %d : %d avis", id, got[id])
		}
	}
	f, err = e.svc.Facts(ctx, admin(gid), gid)
	if err != nil {
		t.Fatal(err)
	}
	if !f.Open || f.OpenedHow != HowDate || f.OpenedAt == nil || !f.OpenedAt.Equal(at) {
		t.Fatalf("faits à la date : %+v", f)
	}

	// Plus tard : aucun nouvel avis, la tâche ne repasse pas.
	e.clock.Advance(time.Hour)
	e.tick(t)
	if err := e.svc.Flush(ctx); err != nil {
		t.Fatal(err)
	}
	if n := len(e.mail.notices()); n != len(ids) {
		t.Fatalf("avis en double : %d", n)
	}
}

func TestNoRevealDateTargetsImmediate(t *testing.T) {
	e := newEnv(t)
	gid := e.group(t, "sans-date", nil)
	ids := e.addN(t, gid, "Philomene", 3, participants.RoleNormal)

	// Avant le tirage, la porte est fermée (rien à révéler).
	open, err := NewGate(e.db, e.clock).IsOpen(ctx, gid)
	if err != nil || open {
		t.Fatalf("porte avant tirage : %v %v", open, err)
	}

	o := e.mustDraw(t, gid)
	e.wantOpen(t, gid, o.DrawID, ids)
	if n := len(e.mail.notices()); n != len(ids) {
		t.Fatalf("sans date, %d avis au lieu de %d dès le tirage", n, len(ids))
	}
	f, err := e.svc.Facts(ctx, admin(gid), gid)
	if err != nil {
		t.Fatal(err)
	}
	if !f.Open || f.OpenedHow != HowImmediate || !f.DateLocked {
		t.Fatalf("faits sans date : %+v", f)
	}
	noPairType(t, f)
}

func TestPastDateOpensAtDrawAndCatchesUpAfterRestart(t *testing.T) {
	e := newEnv(t)
	at := t0.Add(time.Hour)
	gid := e.group(t, "rattrapage", &at)
	ids := e.addN(t, gid, "Clothilde", 3, participants.RoleNormal)
	o := e.mustDraw(t, gid)
	e.wantClosed(t, gid, ids)

	// Arrêt du serveur pendant la date : un nouveau service, un
	// planificateur vide ; Restore rattrape l'ouverture.
	e.clock.Set(at.Add(time.Hour))
	e.sched = scheduler.New(e.clock)
	fresh := NewService(e.db, e.clock, e.sched, e.draw, e.mail, Options{})
	if err := fresh.Restore(ctx); err != nil {
		t.Fatal(err)
	}
	e.wantOpen(t, gid, o.DrawID, ids)
	if n := len(e.mail.notices()); n != len(ids) {
		t.Fatalf("%d avis après rattrapage, %d attendus", n, len(ids))
	}
	if err := fresh.Restore(ctx); err != nil {
		t.Fatal(err)
	}
	if n := len(e.mail.notices()); n != len(ids) {
		t.Fatalf("avis en double après un second démarrage : %d", n)
	}
}

func TestRevealFactsRequireAdmin(t *testing.T) {
	e := newEnv(t)
	gid := e.group(t, "faits", nil)
	other := e.group(t, "autre-groupe", nil)
	ids := e.addN(t, gid, "Leocadie", 3, participants.RoleNormal)
	e.mustDraw(t, gid)
	_, err := e.svc.Facts(ctx, participants.Actor{}, gid)
	e.wantKind(t, err, errs.KindUnauthorized)
	_, err = e.svc.Facts(ctx, who(gid, ids[0]), gid)
	e.wantKind(t, err, errs.KindForbidden)
	_, err = e.svc.Facts(ctx, admin(other), gid)
	e.wantKind(t, err, errs.KindForbidden)
}
