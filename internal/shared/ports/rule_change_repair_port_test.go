package ports

import (
	"context"
	"database/sql"
	"errors"
	"reflect"
	"testing"
)

var _ RuleChangeRepair = (*FakeRuleChangeRepair)(nil)

func TestPreviewOnlyCounts(t *testing.T) {
	// Le type d'aperçu ne comporte que des entiers et un booléen.
	rt := reflect.TypeOf(RepairPreview{})
	for i := 0; i < rt.NumField(); i++ {
		if k := rt.Field(i).Type.Kind(); k != reflect.Int && k != reflect.Bool {
			t.Fatalf("champ %s de type %s interdit", rt.Field(i).Name, k)
		}
	}
	f := &FakeRuleChangeRepair{Previews: map[RuleKind]RepairPreview{
		RuleExclusion: {AffectedCount: 3, AlreadyDiscoveredCount: 1, Repairable: true},
	}}
	p, _ := f.Preview(context.Background(), 1, ProposedRule{Kind: RuleExclusion})
	if p.AffectedCount != 3 || p.AlreadyDiscoveredCount != 1 || !p.Repairable {
		t.Fatalf("aperçu inattendu %+v", p)
	}
}

func TestApplyAtomic(t *testing.T) {
	ctx := context.Background()
	rule := ProposedRule{Kind: RuleExclusion, Members: []int64{1, 2}}

	f := &FakeRuleChangeRepair{}
	if err := f.Apply(ctx, 1, rule, func(context.Context, *sql.Tx) error { return nil }); err != nil {
		t.Fatal(err)
	}
	if len(f.Committed) != 1 || f.RepairCalls != 1 {
		t.Fatal("écriture et réparation attendues")
	}

	// La réparation échoue : rien n'est validé.
	f = &FakeRuleChangeRepair{RepairErr: errors.New("irréparable")}
	if err := f.Apply(ctx, 1, rule, func(context.Context, *sql.Tx) error { return nil }); err == nil {
		t.Fatal("erreur attendue")
	}
	if len(f.Committed) != 0 {
		t.Fatal("la règle ne doit pas être validée")
	}

	// L'écriture échoue : pas de réparation.
	f = &FakeRuleChangeRepair{}
	if err := f.Apply(ctx, 1, rule, func(context.Context, *sql.Tx) error { return errors.New("échec") }); err == nil {
		t.Fatal("erreur attendue")
	}
	if f.RepairCalls != 0 || len(f.Committed) != 0 {
		t.Fatal("aucune réparation attendue")
	}
}
