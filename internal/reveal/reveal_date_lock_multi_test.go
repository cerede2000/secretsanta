package reveal

import (
	"testing"
	"time"

	"secretsanta/internal/draw"
	"secretsanta/internal/participants"
	"secretsanta/internal/shared/errs"
)

// AC15 : la date de révélation reste verrouillée dès qu'une cible quelconque
// a été découverte et la tâche planifiée n'ouvre que les cibles non encore
// ouvertes.
func TestRevealDateLockMultiTargetAndTaskOpensOnlyUnopened(t *testing.T) {
	e := newEnv(t)
	at := t0.Add(3 * 24 * time.Hour)
	gid := e.group(t, "verrou-multi", &at)
	e.addN(t, gid, "Ysaline", 2, participants.RoleReceptionSeule)
	more := e.addN(t, gid, "Leonce", 2, participants.RoleOffrePlus)
	e.addN(t, gid, "Aurelien", 3, participants.RoleNormal)
	o := e.mustDraw(t, gid)
	if n := e.sched.Pending(); n != 1 {
		t.Fatalf("%d tâche(s) planifiée(s)", n)
	}

	// Ouverture manuelle : toutes les cibles sont ouvertes à t0.
	if _, err := e.svc.OpenNow(ctx, admin(gid), gid, true); err != nil {
		t.Fatal(err)
	}
	before := e.targetOpenings(t, o.DrawID)
	if len(before) != len(e.rawPairs(t, o.DrawID)) {
		t.Fatalf("%d cibles ouvertes", len(before))
	}
	mailsAfterOpen := len(e.mail.notices())

	// Le tireur à deux cibles découvre la seconde seulement.
	giver := more[0]
	mine, _ := e.draw.MyTargets(ctx, who(gid, giver), gid)
	if len(mine.Targets) != 2 {
		t.Fatalf("deux cibles attendues : %+v", mine)
	}
	disc := mine.Targets[1].PairID
	if ok, err := e.draw.MarkDiscovered(ctx, who(gid, giver), gid, disc); err != nil || !ok {
		t.Fatalf("découverte : %v %v", ok, err)
	}

	// Une seule cible découverte suffit à verrouiller la date.
	err := e.svc.SetRevealDate(ctx, admin(gid), gid, ptr(at.Add(24*time.Hour)))
	wantFrench(t, e.wantKind(t, err, errs.KindConflict))
	if got := e.storedRevealAt(t, gid); got == nil || !got.Equal(at) {
		t.Fatalf("date modifiée : %v", got)
	}

	// Un ajustement remplace deux paires (nouvelles cibles, non ouvertes).
	raw := e.rawPairs(t, o.DrawID)
	var p1, p2 *rawPair
	for i := range raw {
		for j := range raw {
			a, b := raw[i], raw[j]
			if i == j || a.ID == disc || b.ID == disc || a.Giver == b.Giver ||
				a.Giver == b.Receiver || b.Giver == a.Receiver {
				continue
			}
			p1, p2 = &raw[i], &raw[j]
			break
		}
		if p1 != nil {
			break
		}
	}
	if p1 == nil {
		t.Fatal("aucun échange possible")
	}
	var pairs []draw.Pair
	for _, p := range raw {
		switch p.ID {
		case p1.ID:
			pairs = append(pairs, draw.Pair{Giver: p1.Giver, Receiver: p2.Receiver})
		case p2.ID:
			pairs = append(pairs, draw.Pair{Giver: p2.Giver, Receiver: p1.Receiver})
		default:
			pairs = append(pairs, draw.Pair{Giver: p.Giver, Receiver: p.Receiver})
		}
	}
	e.clock.Advance(time.Hour)
	tx, err := e.db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	if err := e.draw.AdjustTx(ctx, tx, gid, pairs); err != nil {
		_ = tx.Rollback()
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	afterAdjust := e.rawPairs(t, o.DrawID)
	opened := e.targetOpenings(t, o.DrawID)
	var fresh []int64
	for _, p := range afterAdjust {
		if _, ok := opened[p.ID]; !ok {
			fresh = append(fresh, p.ID)
		}
	}
	if len(fresh) != 2 {
		t.Fatalf("%d cible(s) non ouverte(s) après ajustement, 2 attendues", len(fresh))
	}

	// La date arrive : la tâche n'ouvre que les deux nouvelles cibles.
	e.clock.Set(at)
	if n := e.tick(t); n != 1 {
		t.Fatalf("la tâche devait s'exécuter une fois (%d)", n)
	}
	now := e.targetOpenings(t, o.DrawID)
	if len(now) != len(afterAdjust) {
		t.Fatalf("%d cibles ouvertes, %d attendues", len(now), len(afterAdjust))
	}
	for id, stamp := range now {
		if old, ok := opened[id]; ok {
			if stamp != old {
				t.Fatalf("cible %d rouverte : %s → %s", id, old, stamp)
			}
			continue
		}
		if stamp != formatTime(at) {
			t.Fatalf("nouvelle cible %d ouverte à %s", id, stamp)
		}
	}
	// La découverte est intacte, aucun avis en double, la date reste
	// verrouillée.
	mine, _ = e.draw.MyTargets(ctx, who(gid, giver), gid)
	found := false
	for _, tg := range mine.Targets {
		if tg.PairID == disc {
			found = true
			if !tg.Discovered {
				t.Fatal("la découverte est revenue en arrière")
			}
		}
	}
	if !found {
		t.Fatal("cible découverte introuvable")
	}
	if n := len(e.mail.notices()); n != mailsAfterOpen {
		t.Fatalf("avis en double : %d au lieu de %d", n, mailsAfterOpen)
	}
	err = e.svc.SetRevealDate(ctx, admin(gid), gid, ptr(at.Add(24*time.Hour)))
	e.wantKind(t, err, errs.KindConflict)
	f, _ := e.svc.Facts(ctx, admin(gid), gid)
	if !f.DateLocked || !f.Open || f.OpenedHow != HowManual || f.Discovered != 1 {
		t.Fatalf("faits : %+v", f)
	}
}
