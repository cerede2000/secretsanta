package groupsettings

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"secretsanta/internal/config"
	"secretsanta/internal/db"
	"secretsanta/internal/groups"
	"secretsanta/internal/shared/clock"
	"secretsanta/internal/shared/ports"
)

type fakeLangs map[string]bool

func (f fakeLangs) Has(l string) bool { return f[l] }

type env struct {
	db    *sql.DB
	bus   *ports.MemoryEventBus
	draw  *ports.FakeDrawState
	svc   *Service
	gid   int64
	slug  string
	pairs int
}

func newEnv(t *testing.T) *env {
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
	sm, err := Migrations()
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Migrate(d, Module, sm); err != nil {
		t.Fatal(err)
	}
	c := clock.NewFake(time.Date(2026, 11, 15, 10, 0, 0, 0, time.UTC))
	gs, err := groups.NewService(d, c, config.NewSecret("mdp-instance"))
	if err != nil {
		t.Fatal(err)
	}
	r, err := gs.Create(context.Background(), groups.CreateInput{InstancePassword: "mdp-instance", ClientKey: "c", Name: "Famille", Slug: "famille"})
	if err != nil {
		t.Fatal(err)
	}
	bus, draw := &ports.MemoryEventBus{}, &ports.FakeDrawState{Done: map[int64]bool{}}
	return &env{db: d, bus: bus, draw: draw, gid: r.Group.ID, slug: "famille",
		svc: NewService(d, c, bus, draw, fakeLangs{"fr": true, "en": true})}
}

func (e *env) in() Input {
	return Input{GroupID: e.gid, Name: "Famille", Budget: "", EventDate: "", GiftInstructions: ""}
}
