package reminderrules

import (
	"context"
	"testing"
	"time"

	"secretsanta/internal/config"
	"secretsanta/internal/db"
	"secretsanta/internal/groups"
	"secretsanta/internal/shared/clock"
)

type env struct {
	svc *Service
	clk *clock.Fake
	gid int64
}

func newEnv(t *testing.T, loc *time.Location, start time.Time) *env {
	t.Helper()
	d, err := db.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = d.Close() })
	gm, err := groups.Migrations()
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Migrate(d, groups.Module, gm); err != nil {
		t.Fatal(err)
	}
	rm, err := Migrations()
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Migrate(d, Module, rm); err != nil {
		t.Fatal(err)
	}
	c := clock.NewFake(start)
	gs, err := groups.NewService(d, c, config.NewSecret("mdp-instance"))
	if err != nil {
		t.Fatal(err)
	}
	r, err := gs.Create(context.Background(), groups.CreateInput{InstancePassword: "mdp-instance", ClientKey: "c", Name: "Famille", Slug: "famille"})
	if err != nil {
		t.Fatal(err)
	}
	return &env{svc: NewService(d, c, loc), clk: c, gid: r.Group.ID}
}

func paris(t *testing.T) *time.Location {
	t.Helper()
	l, err := time.LoadLocation("Europe/Paris")
	if err != nil {
		t.Fatal(err)
	}
	return l
}
