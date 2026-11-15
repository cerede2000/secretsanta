package draw

import (
	"database/sql"
	"reflect"
	"testing"
	"time"

	"secretsanta/internal/participants"
	"secretsanta/internal/shared/errs"
)

// groupRow lit toutes les colonnes de réglage du groupe.
func (e *env) groupRow(t *testing.T, gid int64) []any {
	t.Helper()
	var slug, name, gift, created string
	var budget sql.NullInt64
	var event, reveal sql.NullString
	var version, canView, maxGifts int
	var hash []byte
	if err := e.db.QueryRow(`SELECT slug, name, budget_cents, budget_version, gift_instructions, event_date, reveal_at,
		admin_can_view_pair, max_gifts_per_giver, admin_secret_hash, created_at FROM groups WHERE id = ?`, gid).
		Scan(&slug, &name, &budget, &version, &gift, &event, &reveal, &canView, &maxGifts, &hash, &created); err != nil {
		t.Fatal(err)
	}
	return []any{slug, name, budget, version, gift, event, reveal, canView, maxGifts, hash, created}
}

func TestFullRedrawRequiresConfirmation(t *testing.T) {
	e := newEnv(t)
	gid := e.group(t, "refait-un", 0, nil)
	e.addN(t, gid, "Xavarin", 5, participants.RoleNormal)

	_, err := e.svc.Draw(ctx, admin(gid), gid, Request{})
	e.wantKind(t, err, errs.KindValidation)
	if n := e.count(t, `SELECT COUNT(*) FROM draws`); n != 0 {
		t.Fatal("un tirage non confirmé a été écrit")
	}
	first := e.mustDraw(t, gid)
	before := e.snapshot(t, gid)

	_, err = e.svc.Redraw(ctx, admin(gid), gid, Request{})
	e.wantKind(t, err, errs.KindValidation)
	if after := e.snapshot(t, gid); !reflect.DeepEqual(before, after) {
		t.Fatal("un nouveau tirage non confirmé a modifié la base")
	}
	_, err = e.svc.Draw(ctx, admin(gid), gid, confirmed)
	e.wantKind(t, err, errs.KindConflict)
	_, err = e.svc.Redraw(ctx, who(gid, 1), gid, confirmed)
	e.wantKind(t, err, errs.KindForbidden)
	st, _ := e.svc.Status(ctx, gid)
	if st.DrawID != first.DrawID {
		t.Fatal("le tirage actuel ne doit pas changer")
	}
}

func TestFullRedrawArchivesRearmsAndKeepsSettings(t *testing.T) {
	e := newEnv(t)
	gid := e.group(t, "refait-deux", 0, nil)
	if _, err := e.db.Exec(`UPDATE groups SET budget_cents = 2500, gift_instructions = 'Fait main', event_date = '2026-12-24',
		admin_can_view_pair = 1 WHERE id = ?`, gid); err != nil {
		t.Fatal(err)
	}
	ids := e.addN(t, gid, "Ombeline", 6, participants.RoleNormal)
	first := e.mustDraw(t, gid)

	// Toutes les cibles sont découvertes avant le nouveau tirage.
	for _, id := range ids {
		mine, err := e.svc.MyTargets(ctx, who(gid, id), gid)
		if err != nil {
			t.Fatal(err)
		}
		for _, tg := range mine.Targets {
			if ok, err := e.svc.MarkDiscovered(ctx, who(gid, id), gid, tg.PairID); err != nil || !ok {
				t.Fatalf("découverte : %v %v", ok, err)
			}
		}
	}
	oldPairs := e.rawPairs(t, first.DrawID)
	settings := e.groupRow(t, gid)

	e.clock.Advance(2 * time.Hour)
	second := e.mustRedraw(t, gid)

	if !reflect.DeepEqual(e.groupRow(t, gid), settings) {
		t.Fatal("le nouveau tirage a modifié les réglages du groupe")
	}
	if !reflect.DeepEqual(e.rawPairs(t, first.DrawID), oldPairs) {
		t.Fatal("l'ancien tirage archivé a été modifié")
	}
	for _, id := range ids {
		mine, err := e.svc.MyTargets(ctx, who(gid, id), gid)
		if err != nil {
			t.Fatal(err)
		}
		for _, tg := range mine.Targets {
			if tg.Discovered {
				t.Fatalf("drapeau non réarmé pour la cible %d", tg.PairID)
			}
		}
	}
	f, err := e.svc.AdminFacts(ctx, admin(gid), gid)
	if err != nil {
		t.Fatal(err)
	}
	if f.Discovered != 0 || f.Targets != 6 || f.DrawID != second.DrawID {
		t.Fatalf("faits inattendus après le nouveau tirage : %+v", f)
	}

	hist, err := e.svc.History(ctx, admin(gid), gid)
	if err != nil {
		t.Fatal(err)
	}
	if len(hist) != 2 {
		t.Fatalf("%d entrées, 2 attendues", len(hist))
	}
	cur, old := hist[0], hist[1]
	if cur.DrawID != second.DrawID || cur.Archived || cur.Kind != KindRedraw {
		t.Fatalf("entrée actuelle inattendue : %+v", cur)
	}
	if old.DrawID != first.DrawID || !old.Archived || old.ArchivedAt == nil || !old.ArchivedAt.Equal(t0.Add(2*time.Hour)) ||
		old.PairCount != 6 {
		t.Fatalf("l'ancien tirage n'est pas archivé : %+v", old)
	}
	if len(old.Events) != 2 || old.Events[0].Kind != KindDraw || old.Events[1].Kind != KindArchive {
		t.Fatalf("journal de l'ancien tirage : %+v", old.Events)
	}
}
