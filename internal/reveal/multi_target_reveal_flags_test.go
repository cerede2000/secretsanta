package reveal

import (
	"testing"
	"time"

	"secretsanta/internal/participants"
)

// AC10 : un tireur peut avoir deux cibles dont chacune porte son drapeau de
// révélation, posé une seule fois ; découvrir l'une ne révèle pas l'autre ;
// un nouveau tirage réarme tous les drapeaux.
func TestMultiTargetRevealFlags(t *testing.T) {
	e := newEnv(t)
	at := t0.Add(24 * time.Hour)
	gid := e.group(t, "deux-cibles", &at)
	e.addN(t, gid, "Bertille", 2, participants.RoleReceptionSeule)
	more := e.addN(t, gid, "Evariste", 2, participants.RoleOffrePlus)
	e.addN(t, gid, "Mederic", 2, participants.RoleNormal)
	o := e.mustDraw(t, gid)
	giver := more[0]

	// Avant la date : aucune des deux cibles n'est renvoyée.
	e.wantClosed(t, gid, more)
	e.clock.Set(at)
	e.tick(t)

	mine, err := e.draw.MyTargets(ctx, who(gid, giver), gid)
	if err != nil || !mine.Ready || len(mine.Targets) != 2 {
		t.Fatalf("deux cibles attendues : %+v %v", mine, err)
	}
	a, b := mine.Targets[0].PairID, mine.Targets[1].PairID
	if a == b {
		t.Fatal("les deux cibles partagent un drapeau")
	}
	// Chaque cible est ouverte séparément par la tâche planifiée.
	opened := e.targetOpenings(t, o.DrawID)
	if _, ok := opened[a]; !ok {
		t.Fatal("première cible non ouverte")
	}
	if _, ok := opened[b]; !ok {
		t.Fatal("seconde cible non ouverte")
	}
	if len(opened) != len(e.rawPairs(t, o.DrawID)) {
		t.Fatalf("%d cibles ouvertes, %d attendues", len(opened), len(e.rawPairs(t, o.DrawID)))
	}

	flag := func(id int64) bool {
		for _, p := range e.rawPairs(t, o.DrawID) {
			if p.ID == id {
				return p.Discovered.Valid
			}
		}
		t.Fatalf("cible %d introuvable", id)
		return false
	}

	// Découvrir la première ne révèle pas la seconde.
	if ok, err := e.draw.MarkDiscovered(ctx, who(gid, giver), gid, a); err != nil || !ok {
		t.Fatalf("première découverte : %v %v", ok, err)
	}
	if !flag(a) || flag(b) {
		t.Fatalf("drapeaux : a=%v b=%v", flag(a), flag(b))
	}
	mine, _ = e.draw.MyTargets(ctx, who(gid, giver), gid)
	if !mine.Targets[0].Discovered || mine.Targets[1].Discovered {
		t.Fatalf("drapeaux exposés : %+v", mine.Targets)
	}
	if ok, _ := e.draw.MarkDiscovered(ctx, who(gid, giver), gid, a); ok {
		t.Fatal("le premier drapeau se pose deux fois")
	}
	f, _ := e.svc.Facts(ctx, admin(gid), gid)
	if f.Discovered != 1 {
		t.Fatalf("faits : %+v", f)
	}

	// L'autre tireur « J'offre plus » ne peut pas poser ce drapeau.
	if _, err := e.draw.MarkDiscovered(ctx, who(gid, more[1]), gid, b); err == nil {
		t.Fatal("un autre participant a posé le drapeau d'une cible qui n'est pas la sienne")
	}
	if flag(b) {
		t.Fatal("le second drapeau a été posé par un autre")
	}

	// Seconde cible : son propre drapeau, une seule fois.
	if ok, err := e.draw.MarkDiscovered(ctx, who(gid, giver), gid, b); err != nil || !ok {
		t.Fatalf("seconde découverte : %v %v", ok, err)
	}
	if ok, _ := e.draw.MarkDiscovered(ctx, who(gid, giver), gid, b); ok {
		t.Fatal("le second drapeau se pose deux fois")
	}
	if !flag(a) || !flag(b) {
		t.Fatal("drapeaux perdus")
	}

	// Un nouveau tirage réarme tous les drapeaux.
	o2 := e.mustRedraw(t, gid)
	for _, p := range e.rawPairs(t, o2.DrawID) {
		if p.Discovered.Valid {
			t.Fatalf("drapeau non réarmé : %+v", p)
		}
	}
	mine, _ = e.draw.MyTargets(ctx, who(gid, giver), gid)
	if !mine.Ready || len(mine.Targets) != 2 || mine.Targets[0].Discovered || mine.Targets[1].Discovered {
		t.Fatalf("cibles après nouveau tirage : %+v", mine)
	}
	if ok, err := e.draw.MarkDiscovered(ctx, who(gid, giver), gid, mine.Targets[1].PairID); err != nil || !ok {
		t.Fatalf("découverte après nouveau tirage : %v %v", ok, err)
	}
	mine, _ = e.draw.MyTargets(ctx, who(gid, giver), gid)
	if mine.Targets[0].Discovered || !mine.Targets[1].Discovered {
		t.Fatalf("drapeaux indépendants attendus après nouveau tirage : %+v", mine.Targets)
	}
}
