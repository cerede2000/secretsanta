package enrolment

import (
	"testing"

	"secretsanta/internal/participants"
	"secretsanta/internal/shared/errs"
)

func TestCreateVerifiedRole(t *testing.T) {
	e := newEnv(t)
	ctx := t.Context()
	for i, r := range []participants.DrawRole{"", participants.RoleNormal, participants.RoleReceptionSeule, participants.RoleOffrePlus} {
		c, err := e.svc.CreateVerified(ctx, e.gid, participants.Input{FirstName: "P", Name: string(rune('A' + i)), Role: r}, t0)
		if err != nil {
			t.Fatalf("rôle %q refusé : %v", r, err)
		}
		want := r
		if want == "" {
			want = participants.RoleNormal
		}
		if c.Participant.Role != want {
			t.Fatalf("rôle %q, attendu %q", c.Participant.Role, want)
		}
	}
	for _, r := range []participants.DrawRole{participants.RoleUnSeul, "admin", "NORMAL", "x' OR 1=1"} {
		if _, err := e.svc.CreateVerified(ctx, e.gid, participants.Input{FirstName: "Q", Name: "Z", Role: r}, t0); err == nil {
			t.Fatalf("rôle %q accepté", r)
		}
	}
	var n int
	if err := e.db.QueryRow(`SELECT COUNT(*) FROM participants WHERE first_name='Q'`).Scan(&n); err != nil || n != 0 {
		t.Fatalf("participant créé malgré le refus : %d %v", n, err)
	}
}

// Le rôle est bien enregistré en base, un refus ne laisse ni participant ni
// ligne et l'erreur est une erreur de validation en français.
func TestCreateVerifiedRoleStoredAndRefusalLeavesNothing(t *testing.T) {
	e := newEnv(t)
	ctx := t.Context()
	c, err := e.svc.CreateVerified(ctx, e.gid, participants.Input{FirstName: "Ana", Name: "Roy", Role: participants.RoleOffrePlus}, t0)
	if err != nil {
		t.Fatal(err)
	}
	var stored string
	if err := e.db.QueryRow(`SELECT draw_role FROM participants WHERE id=?`, c.Participant.ID).Scan(&stored); err != nil || stored != "offre_plus" {
		t.Fatalf("rôle en base %q %v", stored, err)
	}
	before := countRows(t, e)
	for _, r := range []participants.DrawRole{participants.RoleUnSeul, "inconnu", " normal", "Reception_Seule"} {
		_, err := e.svc.CreateVerified(ctx, e.gid, participants.Input{FirstName: "Bo", Name: "Neuf", Role: r}, t0)
		if errs.KindOf(err) != errs.KindValidation {
			t.Fatalf("rôle %q : validation attendue, obtenu %v", r, err)
		}
	}
	if countRows(t, e) != before {
		t.Fatal("un refus a laissé des lignes")
	}
}

func countRows(t *testing.T, e *env) int {
	t.Helper()
	var n int
	if err := e.db.QueryRow(`SELECT (SELECT COUNT(*) FROM participants) + (SELECT COUNT(*) FROM access_credentials)`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}
