package draw

import (
	"reflect"
	"testing"

	"secretsanta/internal/participants"
	"secretsanta/internal/shared/errs"
)

func TestMultiTargetVisibility(t *testing.T) {
	e := newEnv(t)
	gid := e.group(t, "multi-un", 0, nil)
	reception := e.addN(t, gid, "Hortensia", 2, participants.RoleReceptionSeule)
	more := e.addN(t, gid, "Gaspardin", 2, participants.RoleOffrePlus)
	normal := e.addN(t, gid, "Xavarin", 2, participants.RoleNormal)
	all := append(append(append([]int64{}, reception...), more...), normal...)
	o := e.mustDraw(t, gid)
	raw := e.rawPairs(t, o.DrawID)

	got := map[int64]int{}
	for _, id := range all {
		mine, err := e.svc.MyTargets(ctx, who(gid, id), gid)
		if err != nil {
			t.Fatal(err)
		}
		if !mine.Ready {
			t.Fatalf("sans date, les cibles sont immédiates : %+v", mine)
		}
		if want := receiversOfRaw(raw, id); !reflect.DeepEqual(targetIDs(mine.Targets), want) {
			t.Fatalf("participant %d : %v au lieu de ses seules cibles %v", id, targetIDs(mine.Targets), want)
		}
		if len(mine.Targets) > 2 {
			t.Fatalf("participant %d : %d cibles, plafond 2", id, len(mine.Targets))
		}
		for _, tg := range mine.Targets {
			got[tg.ReceiverID]++
		}
	}
	for _, id := range reception {
		mine, _ := e.svc.MyTargets(ctx, who(gid, id), gid)
		if len(mine.Targets) != 0 {
			t.Fatalf("une personne en réception seule voit %d cible(s)", len(mine.Targets))
		}
	}
	for _, id := range more {
		mine, _ := e.svc.MyTargets(ctx, who(gid, id), gid)
		if len(mine.Targets) != 2 {
			t.Fatalf("une personne « J'offre plus » devrait avoir deux cibles, %d obtenue(s)", len(mine.Targets))
		}
	}
	for _, id := range all {
		if got[id] != 1 {
			t.Fatalf("le participant %d reçoit %d cadeau(x), 1 attendu", id, got[id])
		}
	}

	// Découvrir une cible ne découvre pas l'autre, et seulement les siennes.
	giver := more[0]
	mine, _ := e.svc.MyTargets(ctx, who(gid, giver), gid)
	first, err := e.svc.MarkDiscovered(ctx, who(gid, giver), gid, mine.Targets[0].PairID)
	if err != nil || !first {
		t.Fatalf("première découverte : %v %v", first, err)
	}
	again, err := e.svc.MarkDiscovered(ctx, who(gid, giver), gid, mine.Targets[0].PairID)
	if err != nil || again {
		t.Fatalf("la découverte ne se pose qu'une fois : %v %v", again, err)
	}
	mine, _ = e.svc.MyTargets(ctx, who(gid, giver), gid)
	if !mine.Targets[0].Discovered || mine.Targets[1].Discovered {
		t.Fatalf("drapeaux indépendants attendus : %+v", mine.Targets)
	}
	_, err = e.svc.MarkDiscovered(ctx, who(gid, more[1]), gid, mine.Targets[1].PairID)
	e.wantKind(t, err, errs.KindNotFound)
	mine, _ = e.svc.MyTargets(ctx, who(gid, giver), gid)
	if mine.Targets[1].Discovered {
		t.Fatal("un autre participant a pu découvrir une cible qui n'est pas la sienne")
	}

	counts, err := e.svc.TargetCounts(ctx, gid)
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range all {
		if counts[id] != len(receiversOfRaw(raw, id)) {
			t.Fatalf("nombre de cibles de %d : %d", id, counts[id])
		}
	}
	if len(counts) != len(all) {
		t.Fatalf("%d entrées, %d attendues", len(counts), len(all))
	}
}
