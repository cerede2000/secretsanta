package adminops_test

import (
	"bytes"
	"context"
	"reflect"
	"strings"
	"testing"

	"secretsanta/internal/adminops"
	"secretsanta/internal/participants"
	"secretsanta/internal/shared/errs"
)

// F10b.AC10 : l'export puis l'import conserve le rôle de chaque participant.
func TestRoundTripKeepsEveryDrawRole(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	src, _ := f.group(t, "source", "Source", "")
	roles := map[string]participants.DrawRole{
		"Alice": participants.RoleNormal, "Bruno": participants.RoleReceptionSeule,
		"Chloé": participants.RoleOffrePlus, "David": participants.RoleUnSeul,
	}
	for first, r := range roles {
		f.person(t, src, first, "Test", "", r)
	}
	var buf bytes.Buffer
	if err := f.ops.ExportJSON(ctx, src, &buf); err != nil {
		t.Fatal(err)
	}
	for _, r := range roles {
		if !strings.Contains(buf.String(), `"draw_role": "`+string(r)+`"`) {
			t.Errorf("l'export n'écrit pas le rôle %s", r)
		}
	}
	dst, _ := f.group(t, "arrivee", "Arrivée", "")
	if _, err := f.ops.Import(ctx, dst, &buf); err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for _, p := range roster(t, f.db, dst) {
		got[p.First] = p.Role
	}
	for first, r := range roles {
		if got[first] != string(r) {
			t.Errorf("%s : rôle %q, %q attendu", first, got[first], r)
		}
	}
}

// F10b.AC10 : un rôle absent (ancien export) vaut normal.
func TestMissingRoleDefaultsToNormal(t *testing.T) {
	f := newFixture(t)
	gid, _ := f.group(t, "famille", "Famille", "")
	old := `{"format":"secretsanta.participants","version":1,"participants":[{"first_name":"Léa","name":"Roux"},{"first_name":"Tom","name":"Roux","draw_role":"offre_plus"}],"households":[]}`
	if _, err := f.ops.Import(context.Background(), gid, strings.NewReader(old)); err != nil {
		t.Fatal(err)
	}
	got := roster(t, f.db, gid)
	if len(got) != 2 || got[0].First != "Léa" || got[0].Role != "normal" || got[1].Role != "offre_plus" {
		t.Errorf("rôles : %+v", got)
	}
}

// F10b.AC10 : un rôle inconnu (ou vide) fait refuser tout le fichier.
func TestUnknownRoleRejectsWholeFile(t *testing.T) {
	f := newFixture(t)
	gid, _ := f.group(t, "famille", "Famille", "")
	f.person(t, gid, "Anne", "Leroy", "", "")
	before := roster(t, f.db, gid)
	for _, bad := range []string{"chef", "", "NORMAL", "un-seul"} {
		file := validFile(
			adminops.ExchangePerson{FirstName: "Luc", Name: "Bernard", DrawRole: roleOf("normal")},
			adminops.ExchangePerson{FirstName: "Inès", Name: "Moreau", DrawRole: roleOf(bad)},
		)
		file.Households = []adminops.ExchangeHousehold{{Name: "Foyer", Members: []int{0, 1}}}
		_, err := f.ops.Import(context.Background(), gid, bytes.NewReader(encode(t, file)))
		if code, params := errs.CodeOf(err); code != adminops.CodeImportRole || params["n"] != 2 {
			t.Errorf("rôle %q : %v (code %q, %v)", bad, err, code, params)
		}
		if got := roster(t, f.db, gid); !reflect.DeepEqual(got, before) {
			t.Fatalf("rôle %q : import partiel %v", bad, got)
		}
		if n := f.count(t, `SELECT COUNT(*) FROM households WHERE group_id = ?`, gid); n != 0 {
			t.Fatalf("rôle %q : foyer importé", bad)
		}
	}
}
