package repair

import (
	"testing"

	"secretsanta/internal/participants"
	"secretsanta/internal/rulechange"
)

// prioritySetup : x (normal) offre à a et b ; o (offre_plus) et m (normal)
// offrent un cadeau chacun ; a est en réception seule.
func prioritySetup(t *testing.T) (*env, []int64, []Pair) {
	t.Helper()
	e := newEnv(t, 2)
	n := participants.RoleNormal
	ids := e.people(t, n, participants.RoleReceptionSeule, n, participants.RoleOffrePlus, n)
	x, a, b, o, m := ids[0], ids[1], ids[2], ids[3], ids[4]
	before := []Pair{{Giver: x, Receiver: a}, {Giver: x, Receiver: b}, {Giver: o, Receiver: x}, {Giver: m, Receiver: o}, {Giver: b, Receiver: m}}
	e.setPairs(t, before)
	return e, ids, before
}

func giftsOf(pairs []Pair, id int64) int {
	k := 0
	for _, p := range pairs {
		if p.Giver == id {
			k++
		}
	}
	return k
}

// F4c.AC14 : un tireur à deux cadeaux qui passe un_seul est remplacé par un
// offre_plus, en ne changeant qu'une paire.
func TestRoleChangePriorityToUnSeul(t *testing.T) {
	e, ids, before := prioritySetup(t)
	x, o := ids[0], ids[3]
	p, err := e.svc.PreviewRole(ctx, e.gid, x, participants.RoleUnSeul)
	if err != nil || !p.Repairable || p.AffectedCount != 2 {
		t.Fatalf("aperçu %+v %v", p, err)
	}
	if _, err := e.rc.SetRole(ctx, e.gid, x, participants.RoleUnSeul, rulechange.ChoiceRepair); err != nil {
		t.Fatal(err)
	}
	after := e.pairs(t)
	checkValid(t, after, e.roles(t, ids), 2)
	if changedCount(before, after) != 1 {
		t.Fatalf("une seule paire doit changer : %v", after)
	}
	if giftsOf(after, x) != 1 || giftsOf(after, o) != 2 {
		t.Fatalf("la cible libérée doit aller à l'offre_plus : %v", after)
	}
	e.checkMails(t, []int64{x, o})
}

// F4c.AC14 : passage en reception_seule : les deux cibles sont reroutées, le
// cadeau supplémentaire va d'abord à l'offre_plus.
func TestRoleChangePriorityToReceptionOnly(t *testing.T) {
	e, ids, before := prioritySetup(t)
	x, o := ids[0], ids[3]
	if _, err := e.rc.SetRole(ctx, e.gid, x, participants.RoleReceptionSeule, rulechange.ChoiceRepair); err != nil {
		t.Fatal(err)
	}
	after := e.pairs(t)
	roles := e.roles(t, ids)
	checkValid(t, after, roles, 2)
	_, best, _ := bruteOptimum(ids, roles, 2, nil, nil, before, nil)
	if changedCount(before, after) != best {
		t.Fatalf("%d paires changées, minimum %d", changedCount(before, after), best)
	}
	if giftsOf(after, x) != 0 || giftsOf(after, o) != 2 {
		t.Fatalf("l'offre_plus doit porter le cadeau supplémentaire : %v", after)
	}
	e.checkMails(t, givers(before, after))
}

// F4c.AC14 : sans réparation possible (capacité insuffisante), le
// changement de rôle est conservé pour les futurs ajustements seulement.
func TestRoleChangePriorityFallbackFutureOnly(t *testing.T) {
	for _, target := range []participants.DrawRole{participants.RoleUnSeul, participants.RoleReceptionSeule} {
		t.Run(string(target), func(t *testing.T) {
			e := newEnv(t, 2)
			ids := e.people(t, participants.RoleNormal, participants.RoleUnSeul, participants.RoleReceptionSeule)
			x, a, b := ids[0], ids[1], ids[2]
			before := []Pair{{Giver: x, Receiver: a}, {Giver: x, Receiver: b}, {Giver: a, Receiver: x}}
			e.setPairs(t, before)

			p, err := e.svc.PreviewRole(ctx, e.gid, x, target)
			if err != nil || p.Repairable || p.AffectedCount == 0 {
				t.Fatalf("aperçu %+v %v", p, err)
			}
			o, err := e.rc.SetRole(ctx, e.gid, x, target, rulechange.ChoiceRepair)
			if err != nil || o.Status != rulechange.StatusSavedFutureOnly || o.Repaired {
				t.Fatalf("%+v %v", o, err)
			}
			if e.roleOf(t, x) != target {
				t.Fatal("le rôle doit être conservé pour le futur")
			}
			samePairSet(t, e.pairs(t), before)
			if len(e.mail.Sent) != 0 {
				t.Fatal("aucun courriel")
			}
			if flags, _ := e.rc.Flags(ctx, e.gid); len(flags) != 1 {
				t.Fatalf("indicateur attendu : %+v", flags)
			}
		})
	}
}
