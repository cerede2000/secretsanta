package rules

import (
	"context"
	"testing"

	"secretsanta/internal/shared/errs"
)

func TestOneHouseholdPerParticipant(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	if _, err := e.svc.CreateHousehold(ctx, e.gid, "A", []int64{1, 2}); err != nil {
		t.Fatal(err)
	}
	_, err := e.svc.CreateHousehold(ctx, e.gid, "B", []int64{2, 3})
	if !errs.Is(err, errs.KindConflict) {
		t.Fatalf("second foyer accepté : %v", err)
	}
	// Rien n'a été créé pour le foyer refusé (transaction annulée).
	var n int
	_ = e.db.QueryRow(`SELECT COUNT(*) FROM households`).Scan(&n)
	if n != 1 {
		t.Fatalf("%d foyers, attendu 1", n)
	}
	h, err := e.svc.CreateHousehold(ctx, e.gid, "C", []int64{3})
	if err != nil {
		t.Fatal(err)
	}
	if err := e.svc.AddMember(ctx, e.gid, h, 1); !errs.Is(err, errs.KindConflict) {
		t.Fatalf("ajout à un second foyer accepté : %v", err)
	}
	// Garde en base : clé primaire sur participant_id.
	if _, err := e.db.Exec(`INSERT INTO household_members(participant_id, household_id) VALUES (1, ?)`, h); err == nil {
		t.Fatal("la base accepte un second foyer")
	}
}
