package reveal

import (
	"testing"
	"time"

	"secretsanta/internal/participants"
)

// AC7 : le drapeau de révélation n'est posé qu'une fois côté serveur, reste
// posé ensuite et est réarmé quand le tirage est refait.
func TestRevealFlagSetOnceStaysAndRearmsOnRedraw(t *testing.T) {
	e := newEnv(t)
	gid := e.group(t, "drapeau", nil)
	ids := e.addN(t, gid, "Eudoxie", 4, participants.RoleNormal)
	o := e.mustDraw(t, gid)
	giver := ids[0]

	mine, err := e.draw.MyTargets(ctx, who(gid, giver), gid)
	if err != nil || len(mine.Targets) != 1 || mine.Targets[0].Discovered {
		t.Fatalf("cible avant découverte : %+v %v", mine, err)
	}
	pairID := mine.Targets[0].PairID

	first, err := e.draw.MarkDiscovered(ctx, who(gid, giver), gid, pairID)
	if err != nil || !first {
		t.Fatalf("première découverte : %v %v", first, err)
	}
	stamp := func() string {
		for _, p := range e.rawPairs(t, o.DrawID) {
			if p.ID == pairID {
				if !p.Discovered.Valid {
					t.Fatal("drapeau absent")
				}
				return p.Discovered.String
			}
		}
		t.Fatal("cible introuvable")
		return ""
	}
	set := stamp()

	// Seconde découverte, plus tard : rien ne change.
	e.clock.Advance(time.Hour)
	again, err := e.draw.MarkDiscovered(ctx, who(gid, giver), gid, pairID)
	if err != nil || again {
		t.Fatalf("le drapeau ne se pose qu'une fois : %v %v", again, err)
	}
	if got := stamp(); got != set {
		t.Fatalf("le drapeau a bougé : %s → %s", set, got)
	}

	// Le drapeau ne revient jamais en arrière, même en base.
	if _, err := e.db.Exec(`UPDATE draw_pairs SET discovered_at = NULL WHERE id = ?`, pairID); err == nil {
		t.Fatal("le drapeau a pu être effacé")
	}
	// Une ouverture manuelle ou la tâche planifiée ne le touchent pas non
	// plus.
	if _, err := e.svc.OpenNow(ctx, admin(gid), gid, true); err != nil {
		t.Fatal(err)
	}
	e.tick(t)
	if got := stamp(); got != set {
		t.Fatalf("le drapeau a bougé : %s → %s", set, got)
	}
	f, err := e.svc.Facts(ctx, admin(gid), gid)
	if err != nil || f.Discovered != 1 || f.Targets != len(ids) {
		t.Fatalf("faits : %+v %v", f, err)
	}

	// Nouveau tirage : tous les drapeaux sont réarmés.
	o2 := e.mustRedraw(t, gid)
	for _, p := range e.rawPairs(t, o2.DrawID) {
		if p.Discovered.Valid {
			t.Fatalf("drapeau non réarmé après le nouveau tirage : %+v", p)
		}
	}
	f, err = e.svc.Facts(ctx, admin(gid), gid)
	if err != nil || f.Discovered != 0 || f.Targets != len(ids) {
		t.Fatalf("faits après nouveau tirage : %+v %v", f, err)
	}
	// L'ancien tirage archivé garde sa découverte.
	if n := e.count(t, `SELECT COUNT(*) FROM draw_pairs WHERE draw_id = ? AND discovered_at IS NOT NULL`, o.DrawID); n != 1 {
		t.Fatalf("archive : %d découverte(s)", n)
	}
	// Le drapeau réarmé se pose de nouveau, une fois.
	mine, err = e.draw.MyTargets(ctx, who(gid, giver), gid)
	if err != nil {
		t.Fatal(err)
	}
	if !mine.Ready || len(mine.Targets) != 1 || mine.Targets[0].Discovered {
		t.Fatalf("cible réarmée : %+v", mine)
	}
	first, err = e.draw.MarkDiscovered(ctx, who(gid, giver), gid, mine.Targets[0].PairID)
	if err != nil || !first {
		t.Fatalf("découverte après nouveau tirage : %v %v", first, err)
	}
	again, _ = e.draw.MarkDiscovered(ctx, who(gid, giver), gid, mine.Targets[0].PairID)
	if again {
		t.Fatal("le drapeau réarmé se pose deux fois")
	}
}
