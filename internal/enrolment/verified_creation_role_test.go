package enrolment

import (
	"testing"

	"secretsanta/internal/participants"
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
