package participants

import (
	"reflect"
	"testing"
)

func TestRosterRolesAndCounts(t *testing.T) {
	e := newEnv(t)
	roles := []DrawRole{RoleNormal, RoleNormal, RoleReceptionSeule, RoleOffrePlus, RoleUnSeul, RoleUnSeul}
	for i, r := range roles {
		p := e.add(t, "P", string(rune('A'+i)))
		if err := e.svc.SetRole(t.Context(), e.admin(), e.g1, p.ID, r); err != nil {
			t.Fatal(err)
		}
	}
	AdminOther := AdminActor(e.g2)
	if _, err := e.svc.Create(t.Context(), AdminOther, e.g2, Input{FirstName: "Z", Name: "Z"}); err != nil {
		t.Fatal(err)
	}
	r, err := e.svc.Roster(t.Context(), e.g1)
	if err != nil {
		t.Fatal(err)
	}
	want := RoleCounts{Beneficiaries: 6, Givers: 5, ReceptionSeule: 1, OffrePlus: 1, Normal: 2, UnSeul: 2}
	if r.Counts != want {
		t.Fatalf("décomptes %+v, attendu %+v", r.Counts, want)
	}
	if len(r.Entries) != 6 {
		t.Fatalf("%d lignes", len(r.Entries))
	}
	for i, en := range r.Entries {
		if en.Role != roles[i] {
			t.Errorf("ligne %d : %s", i, en.Role)
		}
	}
}

func TestRosterHasNoNameField(t *testing.T) {
	for _, typ := range []reflect.Type{reflect.TypeOf(RosterEntry{}), reflect.TypeOf(Roster{}), reflect.TypeOf(RoleCounts{})} {
		for i := 0; i < typ.NumField(); i++ {
			switch typ.Field(i).Name {
			case "Name", "FirstName", "Email", "GiftHint":
				t.Errorf("%s expose %s", typ, typ.Field(i).Name)
			}
		}
	}
}
