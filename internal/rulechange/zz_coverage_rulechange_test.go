package rulechange

import (
	"context"
	"testing"
)

func TestCoverageRuleErrorsAndRemovals(t *testing.T) {
	e := newEnv(t, false)
	ctx := context.Background()
	a, b, c, d := e.ids[0], e.ids[1], e.ids[2], e.ids[3]

	if _, err := e.svc.AddExclusion(ctx, e.gid, a, a, ChoiceAsk); err == nil {
		t.Fatal("soi-même")
	}
	if _, err := e.svc.AddExclusion(ctx, e.gid, a, 9999, ChoiceAsk); err == nil {
		t.Fatal("inconnu")
	}
	if _, err := e.svc.AddForced(ctx, e.gid, a, a, ChoiceAsk); err == nil {
		t.Fatal("forçage soi-même")
	}
	if _, err := e.svc.AddForced(ctx, e.gid, 9999, a, ChoiceAsk); err == nil {
		t.Fatal("tireur inconnu")
	}
	if _, err := e.svc.AddForced(ctx, e.gid, a, 9999, ChoiceAsk); err == nil {
		t.Fatal("bénéficiaire inconnu")
	}
	if _, err := e.svc.AddExclusion(ctx, e.gid, a, b, ChoiceAsk); err != nil {
		t.Fatal(err)
	}
	if _, err := e.svc.AddForced(ctx, e.gid, a, b, ChoiceAsk); err == nil {
		t.Fatal("forçage contre exclusion")
	}
	if _, err := e.svc.AddForced(ctx, e.gid, b, c, ChoiceAsk); err != nil {
		t.Fatal(err)
	}
	if _, err := e.svc.AddExclusion(ctx, e.gid, b, c, ChoiceAsk); err == nil {
		t.Fatal("exclusion contre forçage")
	}
	if err := e.svc.RemoveForced(ctx, e.gid, b, c); err != nil {
		t.Fatal(err)
	}
	if err := e.svc.RemoveExclusion(ctx, e.gid, a, b); err != nil {
		t.Fatal(err)
	}

	// foyers
	if _, err := e.svc.CreateHousehold(ctx, e.gid, "", []int64{a}, ChoiceAsk); err == nil {
		t.Fatal("nom vide")
	}
	if _, err := e.svc.CreateHousehold(ctx, e.gid, "F", []int64{a, a}, ChoiceAsk); err == nil {
		t.Fatal("doublon")
	}
	if _, err := e.svc.CreateHousehold(ctx, e.gid, "F", []int64{9999}, ChoiceAsk); err == nil {
		t.Fatal("inconnu")
	}
	out, err := e.svc.CreateHousehold(ctx, e.gid, "F", []int64{a, b}, ChoiceAsk)
	if err != nil {
		t.Fatal(err)
	}
	h := out.HouseholdID
	if _, err := e.svc.CreateHousehold(ctx, e.gid, "G", []int64{b}, ChoiceAsk); err == nil {
		t.Fatal("déjà dans un foyer")
	}
	if _, err := e.svc.AddForced(ctx, e.gid, a, b, ChoiceAsk); err == nil {
		t.Fatal("forçage dans un foyer")
	}
	if _, err := e.svc.AddHouseholdMember(ctx, e.gid, 9999, c, ChoiceAsk); err == nil {
		t.Fatal("foyer inconnu")
	}
	if _, err := e.svc.AddHouseholdMember(ctx, e.gid, h, 9999, ChoiceAsk); err == nil {
		t.Fatal("participant inconnu")
	}
	if _, err := e.svc.AddHouseholdMember(ctx, e.gid, h, a, ChoiceAsk); err == nil {
		t.Fatal("déjà membre")
	}
	if _, err := e.svc.AddHouseholdMember(ctx, e.gid, h, c, ChoiceAsk); err != nil {
		t.Fatal(err)
	}
	if err := e.svc.RemoveHouseholdMember(ctx, e.gid, 9999, c); err == nil {
		t.Fatal("foyer inconnu")
	}
	if err := e.svc.RemoveHouseholdMember(ctx, e.gid, h, d); err == nil {
		t.Fatal("non membre")
	}
	if err := e.svc.RemoveHouseholdMember(ctx, e.gid, h, c); err != nil {
		t.Fatal(err)
	}
	// forçage puis foyer / ajout de membre contredisant
	if _, err := e.svc.AddForced(ctx, e.gid, c, d, ChoiceAsk); err != nil {
		t.Fatal(err)
	}
	if _, err := e.svc.CreateHousehold(ctx, e.gid, "H", []int64{c, d}, ChoiceAsk); err == nil {
		t.Fatal("foyer contre forçage")
	}
	h2, err := e.svc.CreateHousehold(ctx, e.gid, "H", []int64{c}, ChoiceAsk)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.svc.AddHouseholdMember(ctx, e.gid, h2.HouseholdID, d, ChoiceAsk); err == nil {
		t.Fatal("ajout contre forçage")
	}
	if err := e.svc.ClearFlags(ctx, e.gid); err != nil {
		t.Fatal(err)
	}
	if fl, err := e.svc.Flags(ctx, e.gid); err != nil || len(fl) != 0 {
		t.Fatalf("flags %v %v", fl, err)
	}
}

func TestCoverageAfterDrawFlagsAndRemovals(t *testing.T) {
	e := newEnv(t, true)
	ctx := context.Background()
	a, b := e.ids[0], e.ids[1]
	for _, ch := range []Choice{ChoiceAsk, ChoiceFutureOnly, ChoiceRepair} {
		if _, err := e.svc.AddExclusion(ctx, e.gid, a, b, ch); err != nil {
			t.Fatal(err)
		}
		if _, err := e.svc.AddHouseholdMember(ctx, e.gid, e.mustHousehold(t, ch), e.ids[2], ch); err != nil {
			t.Log(err)
		}
	}
	if err := e.svc.RemoveExclusion(ctx, e.gid, a, b); err != nil {
		t.Fatal(err)
	}
	if err := e.svc.ClearFlags(ctx, e.gid); err != nil {
		t.Fatal(err)
	}
}

func (e *env) mustHousehold(t *testing.T, ch Choice) int64 {
	t.Helper()
	var id int64
	if err := e.db.QueryRow(`SELECT id FROM households WHERE group_id=? LIMIT 1`, e.gid).Scan(&id); err == nil {
		return id
	}
	res, err := e.db.Exec(`INSERT INTO households(group_id, name) VALUES (?, 'X')`, e.gid)
	if err != nil {
		t.Fatal(err)
	}
	id, _ = res.LastInsertId()
	return id
}
