package participants

import (
	"database/sql"
	"testing"
	"time"

	"secretsanta/internal/db"
	"secretsanta/internal/groups"
	"secretsanta/internal/shared/clock"
	"secretsanta/internal/shared/errs"
	"secretsanta/internal/shared/ports"
)

var t0 = time.Date(2026, 11, 15, 10, 0, 0, 0, time.UTC)

type env struct {
	db    *sql.DB
	clock *clock.Fake
	draw  *ports.FakeDrawState
	svc   *Service
	g1    int64
	g2    int64
}

func migrate(t *testing.T, d *sql.DB, upTo int) {
	t.Helper()
	gm, err := groups.Migrations()
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Migrate(d, groups.Module, gm); err != nil {
		t.Fatal(err)
	}
	pm, err := Migrations()
	if err != nil {
		t.Fatal(err)
	}
	if upTo > 0 {
		pm = pm[:upTo]
	}
	if err := db.Migrate(d, Module, pm); err != nil {
		t.Fatal(err)
	}
}

func insertGroup(t *testing.T, d *sql.DB, slug string) int64 {
	t.Helper()
	res, err := d.Exec(`INSERT INTO groups (slug, name, admin_secret_hash, created_at) VALUES (?, 'G', zeroblob(32), ?)`,
		slug, t0.Format(time.RFC3339))
	if err != nil {
		t.Fatal(err)
	}
	id, _ := res.LastInsertId()
	return id
}

func newEnv(t *testing.T) *env {
	t.Helper()
	d, err := db.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = d.Close() })
	migrate(t, d, 0)
	c := clock.NewFake(t0)
	draw := &ports.FakeDrawState{Done: map[int64]bool{}}
	e := &env{db: d, clock: c, draw: draw, svc: NewService(d, c, draw)}
	e.g1 = insertGroup(t, d, "groupe-un")
	e.g2 = insertGroup(t, d, "groupe-deux")
	return e
}

func (e *env) admin() Actor { return AdminActor(e.g1) }

func (e *env) add(t *testing.T, first, name string) *Participant {
	t.Helper()
	p, err := e.svc.Create(t.Context(), e.admin(), e.g1, Input{FirstName: first, Name: name})
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func wantKind(t *testing.T, err error, k errs.Kind) {
	t.Helper()
	if err == nil {
		t.Fatalf("erreur %s attendue, aucune", k)
	}
	if !errs.Is(err, k) {
		t.Fatalf("nature %s attendue, obtenu %v", k, err)
	}
}
