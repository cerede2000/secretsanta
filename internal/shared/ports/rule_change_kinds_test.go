package ports

import (
	"context"
	"testing"
)

func TestFourKinds(t *testing.T) {
	want := []RuleKind{RuleExclusion, RuleHousehold, RuleForcing, RuleRoleChange}
	if len(AllRuleKinds) != 4 {
		t.Fatalf("quatre natures attendues, obtenu %d", len(AllRuleKinds))
	}
	f := &FakeRuleChangeRepair{}
	for i, k := range want {
		if AllRuleKinds[i] != k {
			t.Fatalf("nature %d: %q", i, AllRuleKinds[i])
		}
		f.Previews = map[RuleKind]RepairPreview{k: {AffectedCount: i + 1, AlreadyDiscoveredCount: i, Repairable: true}}
		p, err := f.Preview(context.Background(), 1, ProposedRule{Kind: k, Members: []int64{1}})
		if err != nil || p.AffectedCount != i+1 || p.AlreadyDiscoveredCount != i {
			t.Fatalf("aperçu %q: %+v %v", k, p, err)
		}
	}
}
