package repair

import (
	"testing"

	"secretsanta/internal/participants"
	"secretsanta/internal/rulechange"
	"secretsanta/internal/shared/ports"
)

// F4c.AC13 : passage en reception_seule après le tirage : aperçu en comptes,
// réparation minimale, courriels aux seuls concernés, sans nouveau tirage.
func TestRoleChangeToReceptionOnly(t *testing.T) {
	e := newEnv(t, 2)
	n := participants.RoleNormal
	ids := e.people(t, n, n, n, n)
	x := ids[1]
	before := ring(ids...)
	e.setPairs(t, before)
	e.discover(t, x, ids[2])
	drawID, rev := e.drawRow(t)
	draws := e.count(t, `SELECT COUNT(*) FROM draws WHERE group_id = ?`, e.gid)

	exact, err := e.svc.PreviewRole(ctx, e.gid, x, participants.RoleReceptionSeule)
	if err != nil || !exact.Repairable || exact.AffectedCount < 2 || exact.AlreadyDiscoveredCount != 1 {
		t.Fatalf("aperçu exact %+v %v", exact, err)
	}
	previewCountsOnly(t, exact)
	port, err := e.svc.Preview(ctx, e.gid, ports.ProposedRule{Kind: ports.RuleRoleChange, Members: []int64{x}})
	if err != nil || !port.Repairable || port.AffectedCount < exact.AffectedCount {
		t.Fatalf("l'aperçu du port doit être prudent : %+v %v", port, err)
	}

	o, err := e.rc.SetRole(ctx, e.gid, x, participants.RoleReceptionSeule, rulechange.ChoiceAsk)
	if err != nil || o.Status != rulechange.StatusNeedsConfirmation {
		t.Fatalf("%+v %v", o, err)
	}
	if e.roleOf(t, x) != n || len(e.mail.Sent) != 0 {
		t.Fatal("rien ne doit être écrit avant confirmation")
	}
	o, err = e.rc.SetRole(ctx, e.gid, x, participants.RoleReceptionSeule, rulechange.ChoiceRepair)
	if err != nil || !o.Repaired {
		t.Fatalf("%+v %v", o, err)
	}
	after := e.pairs(t)
	roles := e.roles(t, ids)
	if roles[x] != participants.RoleReceptionSeule {
		t.Fatal("rôle non écrit")
	}
	checkValid(t, after, roles, 2)
	disc := map[Pair]bool{{Giver: x, Receiver: ids[2]}: true}
	_, best, ok := bruteOptimum(ids, roles, 2, nil, nil, before, disc)
	if !ok || changedCount(before, after) != best {
		t.Fatalf("%d paires changées, minimum %d", changedCount(before, after), best)
	}
	if got := len(givers(before, after)); got != exact.AffectedCount {
		t.Fatalf("aperçu %d, réparation %d", exact.AffectedCount, got)
	}
	e.checkMails(t, givers(before, after))
	if id, rev2 := e.drawRow(t); id != drawID || rev2 != rev+1 ||
		e.count(t, `SELECT COUNT(*) FROM draws WHERE group_id = ?`, e.gid) != draws {
		t.Fatal("un changement de rôle ne doit jamais refaire le tirage")
	}
}

// F4c.AC13 : depuis reception_seule vers normal, la personne reçoit une
// cible ; vers offre_plus ou depuis offre_plus, rien ne change.
func TestRoleChangeFromReceptionOnlyAndNoEffect(t *testing.T) {
	e := newEnv(t, 2)
	n := participants.RoleNormal
	ids := e.people(t, n, n, participants.RoleOffrePlus, participants.RoleReceptionSeule)
	a, b, o, r := ids[0], ids[1], ids[2], ids[3]
	// o offre deux cadeaux, r n'en offre aucun.
	before := []Pair{{Giver: a, Receiver: b}, {Giver: b, Receiver: o}, {Giver: o, Receiver: a}, {Giver: o, Receiver: r}}
	e.setPairs(t, before)

	// normal → offre_plus et offre_plus → normal : aucune paire contredite.
	for _, c := range []struct {
		id   int64
		role participants.DrawRole
	}{{a, participants.RoleOffrePlus}, {o, participants.RoleNormal}} {
		p, err := e.svc.PreviewRole(ctx, e.gid, c.id, c.role)
		if err != nil || p != (ports.RepairPreview{Repairable: true}) {
			t.Fatalf("aperçu %+v %v", p, err)
		}
		if _, err := e.rc.SetRole(ctx, e.gid, c.id, c.role, rulechange.ChoiceRepair); err != nil {
			t.Fatal(err)
		}
		if e.roleOf(t, c.id) != c.role {
			t.Fatal("rôle non écrit")
		}
	}
	samePairSet(t, e.pairs(t), before)
	if len(e.mail.Sent) != 0 {
		t.Fatal("aucun courriel sans paire changée")
	}

	// reception_seule → normal : r doit offrir un cadeau.
	p, err := e.svc.PreviewRole(ctx, e.gid, r, n)
	if err != nil || !p.Repairable || p.AffectedCount == 0 {
		t.Fatalf("aperçu %+v %v", p, err)
	}
	if _, err := e.rc.SetRole(ctx, e.gid, r, n, rulechange.ChoiceRepair); err != nil {
		t.Fatal(err)
	}
	after := e.pairs(t)
	roles := e.roles(t, ids)
	checkValid(t, after, roles, 2)
	_, best, _ := bruteOptimum(ids, roles, 2, nil, nil, before, nil)
	if changedCount(before, after) != best {
		t.Fatalf("%d paires changées, minimum %d", changedCount(before, after), best)
	}
	e.checkMails(t, givers(before, after))
	for _, m := range e.mail.Sent {
		if m.To == e.emails[r] {
			return
		}
	}
	t.Fatal("la personne qui reçoit une cible doit être prévenue")
}
