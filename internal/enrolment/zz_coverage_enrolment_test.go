package enrolment

import (
	"context"
	"testing"

	"secretsanta/internal/participants"
)

func TestCoverageCreateVerifiedErrors(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	if _, err := e.svc.CreateVerified(ctx, e.gid, participants.Input{FirstName: "A", Name: "B", Role: participants.RoleUnSeul}, t0); err == nil {
		t.Fatal("rôle un_seul accepté")
	}
	if _, err := e.svc.CreateVerified(ctx, 9999, participants.Input{FirstName: "A", Name: "B"}, t0); err == nil {
		t.Fatal("groupe inconnu accepté")
	}
	if _, err := e.svc.CreateVerified(ctx, e.gid, participants.Input{}, t0); err == nil {
		t.Fatal("saisie vide acceptée")
	}
	c, err := e.svc.CreateVerified(ctx, e.gid, participants.Input{FirstName: "A", Name: "B", Role: participants.RoleOffrePlus}, t0)
	if err != nil || c.Link == "" || c.Code == "" {
		t.Fatalf("%v %v", c, err)
	}
}
