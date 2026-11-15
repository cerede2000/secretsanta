package repair

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	"secretsanta/internal/participants"
	"secretsanta/internal/rulechange"
	"secretsanta/internal/shared/errs"
	"secretsanta/internal/shared/ports"
)

// F4c.AC7 : une exclusion ajoutée après le tirage contredit une paire :
// aperçu en comptes, réparation minimale sous toutes les contraintes, règle
// et réparation dans une seule transaction, seuls les concernés prévenus.
func TestRuleChangeRepairExclusion(t *testing.T) {
	e := newEnv(t, 2)
	n := participants.RoleNormal
	ids := e.people(t, n, n, n, n, n)
	a, b, c := ids[0], ids[1], ids[2]
	before := ring(ids...)
	e.setPairs(t, before)
	// Une règle existante doit rester respectée par la réparation.
	if _, err := e.rc.AddExclusion(ctx, e.gid, a, c, rulechange.ChoiceAsk); err != nil {
		t.Fatal(err)
	}
	e.discover(t, ids[3], ids[4])
	_, rev := e.drawRow(t)

	rule := ports.ProposedRule{Kind: ports.RuleExclusion, Members: []int64{a, b}}
	p, err := e.svc.Preview(ctx, e.gid, rule)
	if err != nil {
		t.Fatal(err)
	}
	previewCountsOnly(t, p)
	if !p.Repairable || p.AffectedCount == 0 || p.AlreadyDiscoveredCount != 0 {
		t.Fatalf("aperçu %+v", p)
	}

	o, err := e.rc.AddExclusion(ctx, e.gid, a, b, rulechange.ChoiceAsk)
	if err != nil || o.Status != rulechange.StatusNeedsConfirmation || o.Preview != p {
		t.Fatalf("%+v %v", o, err)
	}
	if e.count(t, `SELECT COUNT(*) FROM manual_exclusions WHERE giver_id = ? AND receiver_id = ?`, a, b) != 0 {
		t.Fatal("la règle ne doit pas être écrite avant confirmation")
	}
	samePairSet(t, e.pairs(t), before)
	if len(e.mail.Sent) != 0 {
		t.Fatal("aucun courriel avant confirmation")
	}

	o, err = e.rc.AddExclusion(ctx, e.gid, a, b, rulechange.ChoiceRepair)
	if err != nil || !o.Repaired || o.Status != rulechange.StatusSaved {
		t.Fatalf("%+v %v", o, err)
	}
	if e.count(t, `SELECT COUNT(*) FROM manual_exclusions WHERE giver_id = ? AND receiver_id = ?`, a, b) != 1 {
		t.Fatal("règle absente après réparation")
	}
	after := e.pairs(t)
	roles := e.roles(t, ids)
	checkValid(t, after, roles, 2)
	excl := map[Pair]bool{{Giver: a, Receiver: b}: true, {Giver: a, Receiver: c}: true}
	for _, q := range after {
		if excl[q] {
			t.Fatalf("la paire interdite %v subsiste", q)
		}
	}
	disc := map[Pair]bool{{Giver: ids[3], Receiver: ids[4]}: true}
	rl, best, ok := bruteOptimum(ids, roles, 2, excl, nil, before, disc)
	if !ok || rl != 0 || changedCount(before, after) != best {
		t.Fatalf("%d paires changées, minimum %d (ok=%v)", changedCount(before, after), best, ok)
	}
	if got := len(givers(before, after)); got != p.AffectedCount {
		t.Fatalf("aperçu %d personnes, réparation %d", p.AffectedCount, got)
	}
	// Drapeau de la paire non touchée conservé.
	kept := false
	for _, r := range e.rows(t) {
		if r.Giver == ids[3] && r.Receiver == ids[4] {
			kept = r.Discovered.Valid
		}
	}
	if !kept {
		t.Fatal("le drapeau d'une paire non touchée a été réarmé")
	}
	if _, rev2 := e.drawRow(t); rev2 != rev+1 {
		t.Fatalf("révision %d, attendu %d", rev2, rev+1)
	}
	e.checkMails(t, givers(before, after))
	if len(e.archiver.calls) == 0 || !e.archiver.calls[0].InTx {
		t.Fatalf("archivage : %+v", e.archiver.calls)
	}
}

// F4c.AC7 : un foyer et un forçage contraires au tirage sont réparés et les
// cibles déjà découvertes comptées dans l'aperçu.
func TestRuleChangeRepairHouseholdAndForcing(t *testing.T) {
	e := newEnv(t, 2)
	n := participants.RoleNormal
	ids := e.people(t, n, n, n, n)
	a, b, d := ids[0], ids[1], ids[3]
	before := ring(ids...)
	e.setPairs(t, before)
	e.discover(t, a, b)

	p, err := e.svc.Preview(ctx, e.gid, ports.ProposedRule{Kind: ports.RuleHousehold, Members: []int64{a, b}})
	if err != nil || !p.Repairable || p.AlreadyDiscoveredCount != 1 || p.AffectedCount < 1 {
		t.Fatalf("aperçu foyer %+v %v", p, err)
	}
	o, err := e.rc.CreateHousehold(ctx, e.gid, "Maison", []int64{a, b}, rulechange.ChoiceRepair)
	if err != nil || !o.Repaired {
		t.Fatalf("%+v %v", o, err)
	}
	after := e.pairs(t)
	checkValid(t, after, e.roles(t, ids), 2)
	for _, q := range after {
		if (q.Giver == a && q.Receiver == b) || (q.Giver == b && q.Receiver == a) {
			t.Fatalf("paire interdite par le foyer : %v", q)
		}
	}
	excl := map[Pair]bool{{Giver: a, Receiver: b}: true, {Giver: b, Receiver: a}: true}
	disc := map[Pair]bool{{Giver: a, Receiver: b}: true}
	_, best, _ := bruteOptimum(ids, e.roles(t, ids), 2, excl, nil, before, disc)
	if changedCount(before, after) != best {
		t.Fatalf("%d paires changées, minimum %d", changedCount(before, after), best)
	}
	e.checkMails(t, givers(before, after))

	// Forçage : d doit offrir à b.
	e.mail.Sent = nil
	before = after
	o, err = e.rc.AddForced(ctx, e.gid, d, b, rulechange.ChoiceAsk)
	if err != nil {
		t.Fatal(err)
	}
	contains := false
	for _, q := range before {
		if q.Giver == d && q.Receiver == b {
			contains = true
		}
	}
	if contains {
		if o.Status != rulechange.StatusSaved || len(e.mail.Sent) != 0 {
			t.Fatalf("forçage déjà satisfait : %+v", o)
		}
		return
	}
	if o.Status != rulechange.StatusNeedsConfirmation {
		t.Fatalf("%+v", o)
	}
	if o, err = e.rc.AddForced(ctx, e.gid, d, b, rulechange.ChoiceRepair); err != nil || !o.Repaired {
		t.Fatalf("%+v %v", o, err)
	}
	after = e.pairs(t)
	checkValid(t, after, e.roles(t, ids), 2)
	found := false
	for _, q := range after {
		found = found || (q.Giver == d && q.Receiver == b)
		if excl[q] {
			t.Fatalf("le foyer n'est plus respecté : %v", q)
		}
	}
	if !found {
		t.Fatal("forçage non appliqué")
	}
	_, best, _ = bruteOptimum(ids, e.roles(t, ids), 2, excl, map[Pair]bool{{Giver: d, Receiver: b}: true}, before, nil)
	if changedCount(before, after) != best {
		t.Fatalf("%d paires changées, minimum %d", changedCount(before, after), best)
	}
	e.checkMails(t, givers(before, after))
}

// F4c.AC7 : la règle et la réparation partagent une transaction ; un échec
// de la réparation annule la règle, et un échec de l'écriture annule tout.
func TestRuleChangeRepairSingleTransaction(t *testing.T) {
	e := newEnv(t, 2)
	n := participants.RoleNormal
	ids := e.people(t, n, n, n, n)
	before := ring(ids...)
	e.setPairs(t, before)
	_, rev := e.drawRow(t)

	e.archiver.err = errors.New("panne d'archivage")
	if _, err := e.rc.AddExclusion(ctx, e.gid, ids[0], ids[1], rulechange.ChoiceRepair); err == nil {
		t.Fatal("l'échec de la réparation doit remonter")
	}
	if e.count(t, `SELECT COUNT(*) FROM manual_exclusions`) != 0 {
		t.Fatal("la règle a été écrite malgré l'échec de la réparation")
	}
	samePairSet(t, e.pairs(t), before)
	if _, rev2 := e.drawRow(t); rev2 != rev || len(e.mail.Sent) != 0 {
		t.Fatal("rien ne doit changer ni partir")
	}
	e.archiver.err = nil

	// La règle écrite par le rappel est visible de la réparation (même tx).
	rule := ports.ProposedRule{Kind: ports.RuleExclusion, Members: []int64{ids[0], ids[1]}}
	var seenTx bool
	err := e.svc.Apply(ctx, e.gid, rule, func(ctx context.Context, tx *sql.Tx) error {
		seenTx = tx != nil
		return errs.Validation("refus de la règle")
	})
	if err == nil || !seenTx {
		t.Fatalf("erreur d'écriture attendue (tx=%v) : %v", seenTx, err)
	}
	samePairSet(t, e.pairs(t), before)

	// Avec un rappel valide, règle et réparation sont validées ensemble.
	err = e.svc.Apply(ctx, e.gid, rule, func(ctx context.Context, tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `INSERT INTO manual_exclusions(group_id, giver_id, receiver_id) VALUES (?,?,?)`, e.gid, ids[0], ids[1])
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	if e.count(t, `SELECT COUNT(*) FROM manual_exclusions`) != 1 {
		t.Fatal("règle absente")
	}
	checkValid(t, e.pairs(t), e.roles(t, ids), 2)
	if _, rev2 := e.drawRow(t); rev2 != rev+1 {
		t.Fatal("la réparation doit augmenter la révision une seule fois")
	}
}
