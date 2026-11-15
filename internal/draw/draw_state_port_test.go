package draw

import (
	"testing"
	"time"

	"secretsanta/internal/participants"
	"secretsanta/internal/shared/errs"
	"secretsanta/internal/shared/ports"
)

func TestDrawStatePort(t *testing.T) {
	e := newEnv(t)
	gid := e.group(t, "etat-un", 0, nil)
	other := e.group(t, "etat-autre", 0, nil)
	ids := e.addN(t, gid, "Xavarin", 5, participants.RoleNormal)
	var port ports.DrawState = NewState(e.db)

	done, err := port.IsDrawDone(ctx, gid)
	if err != nil || done {
		t.Fatalf("avant le tirage : %v, %v ; « tirage non fait » attendu", done, err)
	}

	first := e.mustDraw(t, gid)
	if done, _ := port.IsDrawDone(ctx, gid); !done {
		t.Fatal("juste après le tirage : « tirage fait » attendu")
	}
	if done, _ := port.IsDrawDone(ctx, other); done {
		t.Fatal("le tirage d'un groupe ne vaut pas pour un autre")
	}
	st, err := NewState(e.db).Status(ctx, gid)
	if err != nil {
		t.Fatal(err)
	}
	if st.DrawID != first.DrawID || st.Kind != KindDraw || st.Revision != 1 || !st.DrawnAt.Equal(t0) || st.Year != 2026 {
		t.Fatalf("état inattendu : %+v", st)
	}

	// Le port branché sur participants déclenche les règles « après tirage ».
	err = e.parts.Delete(ctx, participants.AdminActor(gid), gid, ids[0], true)
	e.wantKind(t, err, errs.KindConflict)

	// Nouveau tirage.
	e.clock.Advance(time.Hour)
	second := e.mustRedraw(t, gid)
	st, _ = NewState(e.db).Status(ctx, gid)
	if !st.Done || st.DrawID != second.DrawID || st.Kind != KindRedraw || st.DrawID == first.DrawID {
		t.Fatalf("le nouveau tirage n'est pas reflété : %+v", st)
	}

	// Ajustement (F4c) : même tirage, révision suivante, toujours fait.
	raw := e.rawPairs(t, second.DrawID)
	pairs := make([]Pair, len(raw))
	for i, p := range raw {
		pairs[i] = Pair{Giver: p.Giver, Receiver: p.Receiver}
	}
	// Échange des destinataires de deux tireurs.
	swapped := false
	for i := 0; i < len(pairs) && !swapped; i++ {
		for j := i + 1; j < len(pairs) && !swapped; j++ {
			if pairs[i].Giver != pairs[j].Receiver && pairs[j].Giver != pairs[i].Receiver {
				pairs[i].Receiver, pairs[j].Receiver = pairs[j].Receiver, pairs[i].Receiver
				swapped = true
			}
		}
	}
	if !swapped {
		t.Fatal("aucun échange possible")
	}
	tx, err := e.db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := e.svc.AdjustTx(ctx, tx, gid, pairs); err != nil {
		_ = tx.Rollback()
		t.Fatalf("ajustement refusé : %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	st, _ = NewState(e.db).Status(ctx, gid)
	if !st.Done || st.DrawID != second.DrawID || st.Revision != 2 {
		t.Fatalf("l'ajustement n'est pas reflété : %+v", st)
	}
	hist, err := e.svc.History(ctx, admin(gid), gid)
	if err != nil {
		t.Fatal(err)
	}
	last := hist[0].Events[len(hist[0].Events)-1]
	if last.Kind != KindAdjustment || last.Revision != 2 {
		t.Fatalf("l'ajustement n'est pas consigné : %+v", hist[0].Events)
	}
	if hist[0].Revision != 2 {
		t.Fatalf("révision %d dans l'historique, 2 attendue", hist[0].Revision)
	}
}

func TestAdjustRequiresCurrentDrawAndCoherence(t *testing.T) {
	e := newEnv(t)
	gid := e.group(t, "etat-deux", 0, nil)
	ids := e.addN(t, gid, "Ombeline", 3, participants.RoleNormal)
	tx, _ := e.db.BeginTx(ctx, nil)
	e.wantKind(t, e.svc.AdjustTx(ctx, tx, gid, []Pair{{ids[0], ids[1]}, {ids[1], ids[2]}, {ids[2], ids[0]}}), errs.KindConflict)
	_ = tx.Rollback()
	e.mustDraw(t, gid)
	tx, _ = e.db.BeginTx(ctx, nil)
	e.wantKind(t, e.svc.AdjustTx(ctx, tx, gid, []Pair{{ids[0], ids[1]}, {ids[2], ids[1]}}), errs.KindValidation)
	e.wantKind(t, e.svc.AdjustTx(ctx, tx, gid, []Pair{{ids[0], ids[0]}}), errs.KindValidation)
	_ = tx.Rollback()
}
