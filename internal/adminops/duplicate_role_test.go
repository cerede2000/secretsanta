package adminops_test

import (
	"context"
	"testing"

	"secretsanta/internal/participants"
)

// F10b.AC11 : la duplication conserve le rôle dans le tirage de chacun.
func TestDuplicateKeepsDrawRoles(t *testing.T) {
	f := newFixture(t)
	src, _ := f.group(t, "famille-2026", "Famille 2026", "")
	roles := map[string]participants.DrawRole{
		"Alice": participants.RoleNormal, "Bruno": participants.RoleReceptionSeule,
		"Chloé": participants.RoleOffrePlus, "David": participants.RoleUnSeul,
	}
	for first, r := range roles {
		f.person(t, src, first, "Test", "", r)
	}
	res, err := f.ops.Duplicate(context.Background(), src)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for _, p := range roster(t, f.db, res.GroupID) {
		got[p.First] = p.Role
	}
	if len(got) != len(roles) {
		t.Fatalf("%d participants copiés", len(got))
	}
	for first, r := range roles {
		if got[first] != string(r) {
			t.Errorf("%s : rôle %q, %q attendu", first, got[first], r)
		}
	}
}
