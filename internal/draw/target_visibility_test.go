package draw

import (
	"fmt"
	"reflect"
	"testing"
	"time"

	"secretsanta/internal/participants"
	"secretsanta/internal/shared/errs"
)

func TestParticipantSeesOnlyOwnTarget(t *testing.T) {
	e := newEnv(t)
	gid := e.group(t, "visible-un", 0, nil)
	ids := e.addN(t, gid, "Xavarin", 6, participants.RoleNormal)
	o := e.mustDraw(t, gid)
	raw := e.rawPairs(t, o.DrawID)

	seen := map[int64]int64{}
	for _, id := range ids {
		got, err := e.svc.MyTargets(ctx, who(gid, id), gid)
		if err != nil {
			t.Fatal(err)
		}
		if !got.Done || !got.Ready {
			t.Fatalf("sans date de révélation, la cible doit être immédiate : %+v", got)
		}
		want := receiversOfRaw(raw, id)
		if !reflect.DeepEqual(targetIDs(got.Targets), want) {
			t.Fatalf("participant %d : cibles %v, attendues %v (les siennes seulement)", id, targetIDs(got.Targets), want)
		}
		for _, tg := range got.Targets {
			if prev, dup := seen[tg.ReceiverID]; dup {
				t.Fatalf("la cible %d est renvoyée à %d et à %d", tg.ReceiverID, prev, id)
			}
			seen[tg.ReceiverID] = id
		}
	}
}

func TestTargetAccessIsRefusedToOthers(t *testing.T) {
	e := newEnv(t)
	gid := e.group(t, "visible-deux", 0, nil)
	other := e.group(t, "visible-autre", 0, nil)
	ids := e.addN(t, gid, "Ombeline", 4, participants.RoleNormal)
	e.addN(t, other, "Quentrix", 3, participants.RoleNormal)
	e.mustDraw(t, gid)

	_, err := e.svc.MyTargets(ctx, admin(gid), gid)
	e.wantKind(t, err, errs.KindForbidden)
	_, err = e.svc.MyTargets(ctx, participants.Actor{}, gid)
	e.wantKind(t, err, errs.KindUnauthorized)
	_, err = e.svc.MyTargets(ctx, who(other, ids[0]), gid)
	e.wantKind(t, err, errs.KindForbidden)
	// Un participant authentifié dans l'autre groupe ne lit pas ce groupe.
	got, err := e.svc.MyTargets(ctx, who(other, ids[0]), other)
	if err != nil {
		t.Fatal(err)
	}
	if got.Done || len(got.Targets) != 0 {
		t.Fatalf("un autre groupe ne doit rien renvoyer : %+v", got)
	}

	// Les réponses visibles par l'administrateur n'ont aucun type de paire.
	noPairType(t, Outcome{})
	noPairType(t, Facts{})
	noPairType(t, CheckReport{})
	noPairType(t, Status{})
}

func TestHistoryDoesNotLeakOthersPairs(t *testing.T) {
	e := newEnv(t)
	gid := e.group(t, "visible-trois", 0, nil)
	ids := e.addN(t, gid, "Gaspardin", 5, participants.RoleNormal)
	first := e.mustDraw(t, gid)
	e.clock.Advance(time.Hour)
	second := e.mustRedraw(t, gid)
	rawFirst, rawSecond := e.rawPairs(t, first.DrawID), e.rawPairs(t, second.DrawID)

	adminHist, err := e.svc.History(ctx, admin(gid), gid)
	if err != nil {
		t.Fatal(err)
	}
	if len(adminHist) != 2 {
		t.Fatalf("%d entrées d'historique, 2 attendues", len(adminHist))
	}
	for _, h := range adminHist {
		if h.MyTargets != nil {
			t.Fatalf("l'administrateur voit des cibles dans l'historique : %+v", h)
		}
	}

	for _, id := range ids {
		hist, err := e.svc.History(ctx, who(gid, id), gid)
		if err != nil {
			t.Fatal(err)
		}
		for _, h := range hist {
			raw := rawSecond
			if h.DrawID == first.DrawID {
				raw = rawFirst
			}
			if want := receiversOfRaw(raw, id); !reflect.DeepEqual(sorted(h.MyTargets), want) {
				t.Fatalf("participant %d, tirage %d : %v au lieu de %v", id, h.DrawID, h.MyTargets, want)
			}
			e.noNames(t, fmt.Sprintf("%+v", h))
		}
	}
	_, err = e.svc.History(ctx, participants.Actor{}, gid)
	e.wantKind(t, err, errs.KindUnauthorized)
}

func TestTargetHiddenBeforeRevealDate(t *testing.T) {
	e := newEnv(t)
	reveal := t0.Add(48 * time.Hour)
	gid := e.group(t, "visible-quatre", 0, &reveal)
	ids := e.addN(t, gid, "Hortensia", 4, participants.RoleNormal)
	o := e.mustDraw(t, gid)

	got, err := e.svc.MyTargets(ctx, who(gid, ids[0]), gid)
	if err != nil {
		t.Fatal(err)
	}
	if !got.Done || got.Ready || len(got.Targets) != 0 {
		t.Fatalf("avant la date, aucune cible ne doit être renvoyée : %+v", got)
	}
	hist, err := e.svc.History(ctx, who(gid, ids[0]), gid)
	if err != nil {
		t.Fatal(err)
	}
	if len(hist) != 1 || hist[0].MyTargets != nil {
		t.Fatalf("avant la date, l'historique ne doit pas porter la cible : %+v", hist)
	}
	pairID := e.rawPairs(t, o.DrawID)[0].ID
	_, err = e.svc.MarkDiscovered(ctx, who(gid, e.rawPairs(t, o.DrawID)[0].Giver), gid, pairID)
	e.wantKind(t, err, errs.KindConflict)

	e.clock.Set(reveal)
	got, err = e.svc.MyTargets(ctx, who(gid, ids[0]), gid)
	if err != nil {
		t.Fatal(err)
	}
	if !got.Ready || !reflect.DeepEqual(targetIDs(got.Targets), receiversOfRaw(e.rawPairs(t, o.DrawID), ids[0])) {
		t.Fatalf("à la date, la cible doit être renvoyée : %+v", got)
	}
}
