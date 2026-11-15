package rules

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"secretsanta/internal/shared/errs"
)

func TestManualExclusionInConstraints(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	if _, err := e.svc.AddExclusion(ctx, e.gid, 1, 2, false); err != nil {
		t.Fatal(err)
	}
	if _, err := e.svc.AddExclusion(ctx, e.gid, 3, 4, true); err != nil {
		t.Fatal(err)
	}
	c, err := e.svc.BuildConstraints(ctx, e.gid)
	if err != nil {
		t.Fatal(err)
	}
	want := []Pair{{1, 2}, {3, 4}, {4, 3}}
	if !reflect.DeepEqual(c.Exclusions, want) {
		t.Fatalf("exclusions = %v", c.Exclusions)
	}
}

func TestForcedContradictingExclusionRefused(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	if _, err := e.svc.AddExclusion(ctx, e.gid, 1, 2, false); err != nil {
		t.Fatal(err)
	}
	err := e.svc.AddForced(ctx, e.gid, 1, 2)
	if !errs.Is(err, errs.KindConflict) || !strings.Contains(err.Error(), "exclusion") {
		t.Fatalf("forçage contradictoire : %v", err)
	}
	if err := e.svc.AddForced(ctx, e.gid, 2, 1); err != nil {
		t.Fatal(err)
	}
	// Exclusion contredisant un forçage existant.
	_, err = e.svc.AddExclusion(ctx, e.gid, 2, 1, false)
	if !errs.Is(err, errs.KindConflict) {
		t.Fatalf("exclusion contradictoire : %v", err)
	}
	// Forçage contredit par un foyer.
	if _, err := e.svc.CreateHousehold(ctx, e.gid, "F", []int64{2, 1}); !errs.Is(err, errs.KindConflict) {
		t.Fatalf("foyer contradictoire : %v", err)
	}
	if _, err := e.svc.CreateHousehold(ctx, e.gid, "F", []int64{3, 4}); err != nil {
		t.Fatal(err)
	}
	if err := e.svc.AddForced(ctx, e.gid, 3, 4); !errs.Is(err, errs.KindConflict) {
		t.Fatalf("forçage contre foyer : %v", err)
	}
	c, _ := e.svc.BuildConstraints(ctx, e.gid)
	if !reflect.DeepEqual(c.Forced, []Pair{{2, 1}}) {
		t.Fatalf("forced = %v", c.Forced)
	}
}
