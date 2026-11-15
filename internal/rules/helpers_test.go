package rules

import (
	"context"
	"database/sql"
	"testing"

	"secretsanta/internal/db"
	"secretsanta/internal/groups"
)

type fakeDir struct{ roles map[int64]string }

func (f *fakeDir) DrawRole(_ context.Context, _, id int64) (string, bool, error) {
	r, ok := f.roles[id]
	if r == "" {
		r = "normal"
	}
	return r, ok, nil
}

type env struct {
	db  *sql.DB
	dir *fakeDir
	svc *Service
	gid int64
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
	rm, err := Migrations()
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Migrate(d, Module, rm); err != nil {
		t.Fatal(err)
	}
	res, err := d.Exec(`INSERT INTO groups(name, slug, admin_secret_hash, created_at) VALUES ('G','groupe-un', zeroblob(32), '2026-01-01T00:00:00Z')`)
	if err != nil {
		t.Fatal(err)
	}
	gid, _ := res.LastInsertId()
	dir := &fakeDir{roles: map[int64]string{1: "", 2: "", 3: "", 4: "", 5: ""}}
	return &env{db: d, dir: dir, svc: NewService(d, dir), gid: gid}
}
