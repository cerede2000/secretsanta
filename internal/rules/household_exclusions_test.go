package rules

import (
	"context"
	"reflect"
	"testing"
)

func TestHouseholdExclusionsBothWaysAndRemoval(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	h, err := e.svc.CreateHousehold(ctx, e.gid, "Les Martin", []int64{1, 2, 3})
	if err != nil {
		t.Fatal(err)
	}
	c, err := e.svc.BuildConstraints(ctx, e.gid)
	if err != nil {
		t.Fatal(err)
	}
	want := []Pair{{1, 2}, {1, 3}, {2, 1}, {2, 3}, {3, 1}, {3, 2}}
	if !reflect.DeepEqual(c.Exclusions, want) {
		t.Fatalf("exclusions = %v, attendu %v", c.Exclusions, want)
	}
	if err := e.svc.RemoveMember(ctx, e.gid, h, 3); err != nil {
		t.Fatal(err)
	}
	c, _ = e.svc.BuildConstraints(ctx, e.gid)
	want = []Pair{{1, 2}, {2, 1}}
	if !reflect.DeepEqual(c.Exclusions, want) {
		t.Fatalf("après retrait : %v, attendu %v", c.Exclusions, want)
	}
	if err := e.svc.DeleteHousehold(ctx, e.gid, h, false); err == nil {
		t.Fatal("suppression sans confirmation acceptée")
	}
	if err := e.svc.DeleteHousehold(ctx, e.gid, h, true); err != nil {
		t.Fatal(err)
	}
	c, _ = e.svc.BuildConstraints(ctx, e.gid)
	if len(c.Exclusions) != 0 {
		t.Fatalf("exclusions restantes : %v", c.Exclusions)
	}
}
