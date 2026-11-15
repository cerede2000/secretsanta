package rules

import (
	"context"
	"reflect"
	"testing"

	"secretsanta/internal/shared/errs"
)

func TestReceptionSeuleRules(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	e.dir.roles[1] = RoleReceptionSeule

	err := e.svc.AddForced(ctx, e.gid, 1, 2)
	if !errs.Is(err, errs.KindConflict) {
		t.Fatalf("forçage accepté : %v", err)
	}
	if err.Error() == "" {
		t.Fatal("explication absente")
	}
	// Forçage dont la personne en réception seule est bénéficiaire : permis.
	if err := e.svc.AddForced(ctx, e.gid, 2, 1); err != nil {
		t.Fatal(err)
	}

	res, err := e.svc.AddExclusion(ctx, e.gid, 1, 3, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Warnings) != 1 {
		t.Fatalf("avertissements = %v", res.Warnings)
	}
	c, _ := e.svc.BuildConstraints(ctx, e.gid)
	if !reflect.DeepEqual(c.Ineffective, []Pair{{1, 3}}) {
		t.Fatalf("ineffective = %v", c.Ineffective)
	}
	// Une exclusion normale n'est pas signalée.
	res, _ = e.svc.AddExclusion(ctx, e.gid, 3, 4, false)
	if len(res.Warnings) != 0 {
		t.Fatalf("avertissement inattendu : %v", res.Warnings)
	}
}
