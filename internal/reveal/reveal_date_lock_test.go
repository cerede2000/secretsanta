package reveal

import (
	"testing"
	"time"

	"secretsanta/internal/participants"
	"secretsanta/internal/shared/errs"
)

func (e *env) storedRevealAt(t *testing.T, gid int64) *time.Time {
	t.Helper()
	g, err := e.groups.ByID(ctx, gid)
	if err != nil {
		t.Fatal(err)
	}
	return g.RevealAt
}

// AC9 : la date est modifiable tant que la révélation n'est pas ouverte,
// avec replanification de la tâche ; une fois ouverte ou une cible
// découverte, la modification est refusée avec un message français et
// aucune découverte ne revient en arrière.
func TestRevealDateEditableUntilOpenThenLocked(t *testing.T) {
	e := newEnv(t)
	first := t0.Add(24 * time.Hour)
	gid := e.group(t, "date-modifiable", &first)
	ids := e.addN(t, gid, "Hippolyte", 4, participants.RoleNormal)
	o := e.mustDraw(t, gid)
	if n := e.sched.Pending(); n != 1 {
		t.Fatalf("%d tâche(s) planifiée(s), 1 attendue", n)
	}

	// Seul l'administrateur du groupe modifie la date.
	later := t0.Add(72 * time.Hour)
	err := e.svc.SetRevealDate(ctx, who(gid, ids[0]), gid, &later)
	e.wantKind(t, err, errs.KindForbidden)
	err = e.svc.SetRevealDate(ctx, participants.Actor{}, gid, &later)
	e.wantKind(t, err, errs.KindUnauthorized)

	// Une date passée ou retirée après le tirage est refusée en français.
	err = e.svc.SetRevealDate(ctx, admin(gid), gid, ptr(t0.Add(-time.Minute)))
	wantFrench(t, e.wantKind(t, err, errs.KindValidation))
	err = e.svc.SetRevealDate(ctx, admin(gid), gid, nil)
	wantFrench(t, e.wantKind(t, err, errs.KindValidation))
	if got := e.storedRevealAt(t, gid); got == nil || !got.Equal(first) {
		t.Fatalf("date modifiée par un refus : %v", got)
	}

	// Repousser la date : enregistrée, tâche replanifiée (une seule).
	if err := e.svc.SetRevealDate(ctx, admin(gid), gid, &later); err != nil {
		t.Fatalf("modification avant ouverture : %v", err)
	}
	if got := e.storedRevealAt(t, gid); got == nil || !got.Equal(later) {
		t.Fatalf("date enregistrée : %v", got)
	}
	if n := e.sched.Pending(); n != 1 {
		t.Fatalf("%d tâche(s) après replanification, 1 attendue", n)
	}

	// L'ancienne date passe : rien ne s'ouvre.
	e.clock.Set(first)
	if n := e.tick(t); n != 0 {
		t.Fatalf("l'ancienne tâche s'est exécutée (%d)", n)
	}
	e.wantClosed(t, gid, ids)
	if n := len(e.mail.notices()); n != 0 {
		t.Fatalf("%d avis à l'ancienne date", n)
	}

	// Avancer la date : de nouveau replanifiée.
	sooner := first.Add(time.Hour)
	if err := e.svc.SetRevealDate(ctx, admin(gid), gid, &sooner); err != nil {
		t.Fatal(err)
	}
	if n := e.sched.Pending(); n != 1 {
		t.Fatalf("%d tâche(s), 1 attendue", n)
	}
	e.clock.Set(sooner)
	if n := e.tick(t); n != 1 {
		t.Fatalf("la tâche replanifiée ne s'est pas exécutée (%d)", n)
	}
	e.wantOpen(t, gid, o.DrawID, ids)
	if n := len(e.mail.notices()); n != len(ids) {
		t.Fatalf("%d avis, %d attendus", n, len(ids))
	}

	// Ouverte : toute modification est refusée, en français.
	err = e.svc.SetRevealDate(ctx, admin(gid), gid, ptr(sooner.Add(48*time.Hour)))
	msg := e.wantKind(t, err, errs.KindConflict)
	wantFrench(t, msg)
	if code, _ := errs.CodeOf(err); code != CodeDateLocked {
		t.Fatalf("code %q", code)
	}
	if got := e.storedRevealAt(t, gid); got == nil || !got.Equal(sooner) {
		t.Fatalf("date modifiée malgré le verrou : %v", got)
	}
	e.wantOpen(t, gid, o.DrawID, ids)
	f, _ := e.svc.Facts(ctx, admin(gid), gid)
	if !f.DateLocked {
		t.Fatalf("faits : %+v", f)
	}
}

func TestRevealDateLockedOnceATargetIsDiscovered(t *testing.T) {
	e := newEnv(t)
	at := t0.Add(5 * 24 * time.Hour)
	gid := e.group(t, "decouverte", &at)
	ids := e.addN(t, gid, "Gontran", 3, participants.RoleNormal)
	o := e.mustDraw(t, gid)

	// Ouverture manuelle puis découverte d'une cible.
	if _, err := e.svc.OpenNow(ctx, admin(gid), gid, true); err != nil {
		t.Fatal(err)
	}
	mine, _ := e.draw.MyTargets(ctx, who(gid, ids[0]), gid)
	if ok, err := e.draw.MarkDiscovered(ctx, who(gid, ids[0]), gid, mine.Targets[0].PairID); err != nil || !ok {
		t.Fatalf("découverte : %v %v", ok, err)
	}

	for _, d := range []*time.Time{ptr(at.Add(24 * time.Hour)), ptr(t0.Add(time.Hour)), nil} {
		err := e.svc.SetRevealDate(ctx, admin(gid), gid, d)
		wantFrench(t, e.wantKind(t, err, errs.KindConflict))
	}
	if got := e.storedRevealAt(t, gid); got == nil || !got.Equal(at) {
		t.Fatalf("date modifiée : %v", got)
	}
	// La découverte ne revient pas en arrière.
	if n := e.count(t, `SELECT COUNT(*) FROM draw_pairs WHERE draw_id = ? AND discovered_at IS NOT NULL`, o.DrawID); n != 1 {
		t.Fatalf("%d découverte(s), 1 attendue", n)
	}
	mine, _ = e.draw.MyTargets(ctx, who(gid, ids[0]), gid)
	if !mine.Ready || !mine.Targets[0].Discovered {
		t.Fatalf("découverte perdue : %+v", mine)
	}
}

func TestRevealDateBeforeDrawAndWithoutDate(t *testing.T) {
	e := newEnv(t)
	gid := e.group(t, "avant-tirage", nil)
	ids := e.addN(t, gid, "Perrine", 3, participants.RoleNormal)

	// Avant le tirage : on peut fixer puis retirer la date.
	at := t0.Add(24 * time.Hour)
	if err := e.svc.SetRevealDate(ctx, admin(gid), gid, &at); err != nil {
		t.Fatal(err)
	}
	if err := e.svc.SetRevealDate(ctx, admin(gid), gid, nil); err != nil {
		t.Fatal(err)
	}
	if e.storedRevealAt(t, gid) != nil || e.sched.Pending() != 0 {
		t.Fatal("la date retirée laisse une trace")
	}

	// Tirage sans date : la révélation est ouverte, la date est verrouillée
	// (fixer une date cacherait des cibles déjà visibles).
	o := e.mustDraw(t, gid)
	e.wantOpen(t, gid, o.DrawID, ids)
	err := e.svc.SetRevealDate(ctx, admin(gid), gid, &at)
	wantFrench(t, e.wantKind(t, err, errs.KindConflict))
	e.wantOpen(t, gid, o.DrawID, ids)
}
