package repair

import (
	"context"
	"database/sql"
	"testing"

	"secretsanta/internal/participants"
	"secretsanta/internal/rulechange"
	"secretsanta/internal/shared/errs"
	"secretsanta/internal/shared/ports"
)

// F4c.AC8 : une règle qui ne contredit aucune paire n'en change aucune.
func TestRuleChangeNoEffect(t *testing.T) {
	e := newEnv(t, 2)
	n := participants.RoleNormal
	ids := e.people(t, n, n, n, n)
	before := ring(ids...)
	e.setPairs(t, before)
	e.discover(t, ids[0], ids[1])
	rowsBefore := e.rows(t)
	_, rev := e.drawRow(t)

	for _, rule := range []ports.ProposedRule{
		{Kind: ports.RuleExclusion, Members: []int64{ids[0], ids[2]}},
		{Kind: ports.RuleHousehold, Members: []int64{ids[0], ids[2]}},
		{Kind: ports.RuleForcing, Members: []int64{ids[0], ids[1]}},
	} {
		p, err := e.svc.Preview(ctx, e.gid, rule)
		if err != nil || p != (ports.RepairPreview{Repairable: true}) {
			t.Fatalf("%s : aperçu %+v %v", rule.Kind, p, err)
		}
	}
	o, err := e.rc.AddExclusion(ctx, e.gid, ids[0], ids[2], rulechange.ChoiceAsk)
	if err != nil || o.Status != rulechange.StatusSaved || o.Repaired {
		t.Fatalf("%+v %v", o, err)
	}
	if _, err := e.rc.CreateHousehold(ctx, e.gid, "Maison", []int64{ids[1], ids[3]}, rulechange.ChoiceAsk); err != nil {
		t.Fatal(err)
	}
	// Même par la voie de la réparation, rien ne bouge.
	o, err = e.rc.AddForced(ctx, e.gid, ids[2], ids[3], rulechange.ChoiceRepair)
	if err != nil || o.Status != rulechange.StatusSaved {
		t.Fatalf("%+v %v", o, err)
	}
	err = e.svc.Apply(ctx, e.gid, ports.ProposedRule{Kind: ports.RuleExclusion, Members: []int64{ids[3], ids[1]}},
		func(ctx context.Context, tx *sql.Tx) error {
			_, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO manual_exclusions(group_id, giver_id, receiver_id) VALUES (?,?,?)`, e.gid, ids[3], ids[1])
			return err
		})
	if err != nil {
		t.Fatal(err)
	}
	if got := e.rows(t); len(got) != len(rowsBefore) {
		t.Fatalf("paires %v", got)
	} else {
		for i := range got {
			if got[i] != rowsBefore[i] {
				t.Fatalf("paire ou drapeau changé : %v → %v", rowsBefore[i], got[i])
			}
		}
	}
	if _, rev2 := e.drawRow(t); rev2 != rev {
		t.Fatal("la révision ne doit pas bouger")
	}
	if len(e.mail.Sent) != 0 || len(e.archiver.calls) != 0 {
		t.Fatal("aucun courriel ni archivage")
	}
	if flags, _ := e.rc.Flags(ctx, e.gid); len(flags) != 0 {
		t.Fatalf("aucun indicateur attendu : %+v", flags)
	}
}

// F4c.AC8 : sans réparation possible, la règle est conservée « n'affecte pas
// le tirage actuel » ; Apply direct refuse sans rien écrire.
func TestRuleChangeNoRepairKeepsRuleForFuture(t *testing.T) {
	e := newEnv(t, 2)
	n := participants.RoleNormal
	ids := e.people(t, n, n, n)
	a, b := ids[0], ids[1]
	before := ring(ids...)
	e.setPairs(t, before)

	// Foyer {a, b} sur 3 personnes : c devrait recevoir deux cadeaux.
	rule := ports.ProposedRule{Kind: ports.RuleHousehold, Members: []int64{a, b}}
	p, err := e.svc.Preview(ctx, e.gid, rule)
	if err != nil || p.Repairable || p.AffectedCount == 0 {
		t.Fatalf("aperçu %+v %v", p, err)
	}
	previewCountsOnly(t, p)
	err = e.svc.Apply(ctx, e.gid, rule, func(ctx context.Context, tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `INSERT INTO households(group_id, name) VALUES (?, 'X')`, e.gid)
		return err
	})
	if err == nil || !errs.Is(err, errs.KindConflict) {
		t.Fatalf("conflit attendu : %v", err)
	}
	if e.count(t, `SELECT COUNT(*) FROM households`) != 0 {
		t.Fatal("rien ne doit être écrit quand aucune réparation n'existe")
	}

	o, err := e.rc.CreateHousehold(ctx, e.gid, "Maison", []int64{a, b}, rulechange.ChoiceRepair)
	if err != nil || o.Status != rulechange.StatusSavedFutureOnly || o.Repaired {
		t.Fatalf("%+v %v", o, err)
	}
	if e.count(t, `SELECT COUNT(*) FROM household_members`) != 2 {
		t.Fatal("le foyer doit être conservé")
	}
	if flags, _ := e.rc.Flags(ctx, e.gid); len(flags) != 1 {
		t.Fatalf("indicateur attendu : %+v", flags)
	}
	samePairSet(t, e.pairs(t), before)
	if len(e.mail.Sent) != 0 {
		t.Fatal("aucun courriel")
	}
}

// F4c.AC8 : l'administrateur refuse la réparation ; la règle vaut ensuite
// pour les futurs ajustements (une réparation ultérieure la respecte).
func TestRuleChangeRefusedAppliesToFutureAdjustments(t *testing.T) {
	e := newEnv(t, 2)
	n := participants.RoleNormal
	ids := e.people(t, n, n, n, n, n)
	before := ring(ids...)
	e.setPairs(t, before)

	o, err := e.rc.AddExclusion(ctx, e.gid, ids[0], ids[1], rulechange.ChoiceFutureOnly)
	if err != nil || o.Status != rulechange.StatusSavedFutureOnly || o.Repaired {
		t.Fatalf("%+v %v", o, err)
	}
	samePairSet(t, e.pairs(t), before)
	if len(e.mail.Sent) != 0 {
		t.Fatal("aucun courriel quand la réparation est refusée")
	}
	if flags, _ := e.rc.Flags(ctx, e.gid); len(flags) != 1 {
		t.Fatalf("indicateur attendu : %+v", flags)
	}

	// Réparation suivante : une autre règle contredit le tirage ; la
	// réparation respecte aussi la règle conservée pour le futur.
	o, err = e.rc.AddExclusion(ctx, e.gid, ids[2], ids[3], rulechange.ChoiceRepair)
	if err != nil || !o.Repaired {
		t.Fatalf("%+v %v", o, err)
	}
	after := e.pairs(t)
	checkValid(t, after, e.roles(t, ids), 2)
	for _, q := range after {
		if (q.Giver == ids[0] && q.Receiver == ids[1]) || (q.Giver == ids[2] && q.Receiver == ids[3]) {
			t.Fatalf("paire interdite %v", q)
		}
	}
	e.checkMails(t, givers(before, after))
}
