package rules

import (
	"context"
	"testing"
)

func TestCoverageHouseholdErrorsAndMembers(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	if _, err := e.svc.CreateHousehold(ctx, e.gid, "", []int64{1}); err == nil {
		t.Fatal("nom vide accepté")
	}
	if _, err := e.svc.CreateHousehold(ctx, e.gid, "F", []int64{99}); err == nil {
		t.Fatal("membre inconnu accepté")
	}
	if _, err := e.svc.CreateHousehold(ctx, e.gid, "F", []int64{1, 1}); err == nil {
		t.Fatal("doublon accepté")
	}
	id, err := e.svc.CreateHousehold(ctx, e.gid, "F", []int64{1, 2})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.svc.CreateHousehold(ctx, e.gid, "G", []int64{2}); err == nil {
		t.Fatal("double appartenance acceptée")
	}
	if err := e.svc.AddMember(ctx, e.gid, 999, 3); err == nil {
		t.Fatal("foyer inconnu")
	}
	if err := e.svc.AddMember(ctx, e.gid, id, 99); err == nil {
		t.Fatal("participant inconnu")
	}
	if err := e.svc.AddMember(ctx, e.gid, id, 2); err == nil {
		t.Fatal("déjà membre")
	}
	if err := e.svc.AddMember(ctx, e.gid, id, 3); err != nil {
		t.Fatal(err)
	}
	m, err := e.svc.Members(ctx, e.gid, id)
	if err != nil || len(m) != 3 {
		t.Fatalf("membres %v %v", m, err)
	}
	if err := e.svc.RemoveMember(ctx, e.gid, 999, 3); err == nil {
		t.Fatal("foyer inconnu")
	}
	if err := e.svc.RemoveMember(ctx, e.gid, id, 5); err == nil {
		t.Fatal("non membre")
	}
	if err := e.svc.RemoveMember(ctx, e.gid, id, 3); err != nil {
		t.Fatal(err)
	}
	if err := e.svc.DeleteHousehold(ctx, e.gid, id, false); err == nil {
		t.Fatal("sans confirmation")
	}
	if err := e.svc.DeleteHousehold(ctx, e.gid, 999, true); err == nil {
		t.Fatal("foyer inconnu")
	}
	if err := e.svc.DeleteHousehold(ctx, e.gid, id, true); err != nil {
		t.Fatal(err)
	}
}

func TestCoverageForcedExclusionAndForget(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	e.dir.roles[5] = RoleReceptionSeule
	if err := e.svc.AddForced(ctx, e.gid, 1, 1); err == nil {
		t.Fatal("soi-même")
	}
	if err := e.svc.AddForced(ctx, e.gid, 99, 1); err == nil {
		t.Fatal("tireur inconnu")
	}
	if err := e.svc.AddForced(ctx, e.gid, 1, 99); err == nil {
		t.Fatal("bénéficiaire inconnu")
	}
	if err := e.svc.AddForced(ctx, e.gid, 5, 1); err == nil {
		t.Fatal("réception seule")
	}
	if _, err := e.svc.AddExclusion(ctx, e.gid, 2, 3, true); err != nil {
		t.Fatal(err)
	}
	if err := e.svc.AddForced(ctx, e.gid, 2, 3); err == nil {
		t.Fatal("exclusion contredite")
	}
	if err := e.svc.AddForced(ctx, e.gid, 1, 2); err != nil {
		t.Fatal(err)
	}
	if _, err := e.svc.AddExclusion(ctx, e.gid, 1, 2, false); err == nil {
		t.Fatal("exclusion contre forçage")
	}
	if _, err := e.svc.CreateHousehold(ctx, e.gid, "X", []int64{1, 2}); err == nil {
		t.Fatal("foyer contre forçage")
	}
	hid, err := e.svc.CreateHousehold(ctx, e.gid, "Y", []int64{3})
	if err != nil {
		t.Fatal(err)
	}
	if err := e.svc.AddMember(ctx, e.gid, hid, 4); err != nil {
		t.Fatal(err)
	}
	if err := e.svc.RemoveMember(ctx, e.gid, hid, 4); err != nil {
		t.Fatal(err)
	}
	if err := e.svc.RemoveForced(ctx, e.gid, 1, 2); err != nil {
		t.Fatal(err)
	}
	if err := e.svc.RemoveExclusion(ctx, e.gid, 2, 3, true); err != nil {
		t.Fatal(err)
	}
	if err := e.svc.AddForced(ctx, e.gid, 3, 4); err != nil {
		t.Fatal(err)
	}
	if _, err := e.svc.AddExclusion(ctx, e.gid, 4, 5, false); err != nil {
		t.Fatal(err)
	}
	if err := e.svc.ForgetParticipant(ctx, e.gid, 3); err != nil {
		t.Fatal(err)
	}
	c, err := e.svc.BuildConstraints(ctx, e.gid)
	if err != nil {
		t.Fatal(err)
	}
	if len(c.Forced) != 0 {
		t.Fatalf("forçage résiduel %v", c.Forced)
	}
}
