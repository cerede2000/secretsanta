package ports

import (
	"context"
	"testing"
)

var _ RegistrationState = (*FakeRegistrationState)(nil)
var _ DrawState = (*FakeDrawState)(nil)

func TestEmptyGroup(t *testing.T) {
	ctx := context.Background()
	rs, ds := &FakeRegistrationState{}, &FakeDrawState{}
	if n, _ := rs.PendingCount(ctx, 1); n != 0 {
		t.Fatalf("attendu 0, obtenu %d", n)
	}
	if done, _ := ds.IsDrawDone(ctx, 1); done {
		t.Fatal("tirage non fait attendu")
	}
}

func TestResolve(t *testing.T) {
	ctx := context.Background()
	for _, how := range []RegistrationResolution{ResolveIgnore, ResolveDelete} {
		rs := &FakeRegistrationState{Pending: map[int64]int{1: 3}}
		if err := rs.Resolve(ctx, 1, how); err != nil {
			t.Fatal(err)
		}
		if rs.Resolved[1] != how {
			t.Fatalf("résolution %q non exposée", how)
		}
		if n, _ := rs.PendingCount(ctx, 1); n != 0 {
			t.Fatalf("attendu 0 après %q", how)
		}
	}
}
