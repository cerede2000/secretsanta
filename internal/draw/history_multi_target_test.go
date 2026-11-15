package draw

import (
	"fmt"
	"reflect"
	"testing"
	"time"

	"secretsanta/internal/participants"
)

func TestHistoryByYearKeepsMultipleTargetsPrivately(t *testing.T) {
	e := newEnv(t)
	gid := e.group(t, "histoire-un", 0, nil)
	reception := e.addN(t, gid, "Hortensia", 2, participants.RoleReceptionSeule)
	more := e.addN(t, gid, "Gaspardin", 2, participants.RoleOffrePlus)
	normal := e.addN(t, gid, "Xavarin", 2, participants.RoleNormal)
	all := append(append(append([]int64{}, reception...), more...), normal...)

	d2026 := e.mustDraw(t, gid)
	e.clock.Advance(time.Hour)
	redo2026 := e.mustRedraw(t, gid)
	e.clock.Set(time.Date(2027, 11, 20, 9, 0, 0, 0, time.UTC))
	d2027 := e.mustRedraw(t, gid)

	raws := map[int64][]rawPair{}
	for _, id := range []int64{d2026.DrawID, redo2026.DrawID, d2027.DrawID} {
		raws[id] = e.rawPairs(t, id)
		if len(raws[id]) != len(all) {
			t.Fatalf("tirage %d : %d paires, %d attendues", id, len(raws[id]), len(all))
		}
		for _, g := range more {
			if n := len(receiversOfRaw(raws[id], g)); n != 2 {
				t.Fatalf("tirage %d : une personne « J'offre plus » a %d cible(s), 2 attendues", id, n)
			}
		}
	}

	adminHist, err := e.svc.History(ctx, admin(gid), gid)
	if err != nil {
		t.Fatal(err)
	}
	years := []int{}
	for _, h := range adminHist {
		years = append(years, h.Year)
		if h.MyTargets != nil {
			t.Fatal("l'administrateur voit des cibles")
		}
		if h.PairCount != len(all) {
			t.Fatalf("tirage %d : %d paires conservées, %d attendues", h.DrawID, h.PairCount, len(all))
		}
	}
	if !reflect.DeepEqual(years, []int{2027, 2026, 2026}) {
		t.Fatalf("années de l'historique : %v", years)
	}
	if adminHist[0].Archived || !adminHist[1].Archived || !adminHist[2].Archived {
		t.Fatalf("statuts : %+v", adminHist)
	}

	for _, id := range all {
		hist, err := e.svc.History(ctx, who(gid, id), gid)
		if err != nil {
			t.Fatal(err)
		}
		if len(hist) != 3 {
			t.Fatalf("%d entrées, 3 attendues", len(hist))
		}
		for _, h := range hist {
			want := receiversOfRaw(raws[h.DrawID], id)
			if !reflect.DeepEqual(sorted(h.MyTargets), want) {
				t.Fatalf("participant %d, tirage %d (%d) : %v au lieu de %v", id, h.DrawID, h.Year, h.MyTargets, want)
			}
			e.noNames(t, fmt.Sprintf("%+v", h))
		}
	}
	for _, id := range reception {
		hist, _ := e.svc.History(ctx, who(gid, id), gid)
		for _, h := range hist {
			if len(h.MyTargets) != 0 {
				t.Fatal("une personne en réception seule a des cibles dans l'historique")
			}
		}
	}
}
