package rulechange

import "testing"

// F3d.AC4 : une règle qui ne contredit aucune paire est enregistrée sans réparation.
func TestRuleAfterDrawNoEffect(t *testing.T) {
	e := newEnv(t, true)
	a, b, c, d := e.ids[0], e.ids[1], e.ids[2], e.ids[3]

	o, err := e.svc.AddExclusion(t.Context(), e.gid, a, b, ChoiceAsk)
	if err != nil || o.Status != StatusSaved || o.Repaired {
		t.Fatalf("exclusion : %+v %v", o, err)
	}
	o, err = e.svc.AddForced(t.Context(), e.gid, c, d, ChoiceAsk)
	if err != nil || o.Status != StatusSaved {
		t.Fatalf("forçage : %+v %v", o, err)
	}
	o, err = e.svc.CreateHousehold(t.Context(), e.gid, "Foyer", []int64{a, d}, ChoiceAsk)
	if err != nil || o.Status != StatusSaved || o.HouseholdID == 0 {
		t.Fatalf("foyer : %+v %v", o, err)
	}
	o, err = e.svc.AddHouseholdMember(t.Context(), e.gid, o.HouseholdID, b, ChoiceAsk)
	if err != nil || o.Status != StatusSaved {
		t.Fatalf("membre : %+v %v", o, err)
	}
	if e.repair.RepairCalls != 0 || len(e.repair.Committed) != 0 {
		t.Fatal("aucune réparation attendue")
	}
	if n := e.count(t, `SELECT COUNT(*) FROM manual_exclusions`); n != 1 {
		t.Fatalf("exclusions = %d", n)
	}
	if n := e.count(t, `SELECT COUNT(*) FROM forced_pairs`); n != 1 {
		t.Fatalf("forçages = %d", n)
	}
	if n := e.count(t, `SELECT COUNT(*) FROM household_members`); n != 3 {
		t.Fatalf("membres = %d", n)
	}
	if f, _ := e.svc.Flags(t.Context(), e.gid); len(f) != 0 {
		t.Fatalf("indicateurs inattendus : %v", f)
	}
}
