package db

import (
	"database/sql"
	"testing"
	"testing/fstest"
)

func open(t *testing.T) *sql.DB {
	t.Helper()
	d, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = d.Close() })
	return d
}

func count(t *testing.T, d *sql.DB, q string) int {
	t.Helper()
	var n int
	if err := d.QueryRow(q).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestPragmas(t *testing.T) {
	d := open(t)
	if got := count(t, d, "PRAGMA foreign_keys"); got != 1 {
		t.Fatalf("foreign_keys = %d", got)
	}
	var mode string
	if err := d.QueryRow("PRAGMA journal_mode").Scan(&mode); err != nil || mode != "wal" {
		t.Fatalf("journal_mode = %q %v", mode, err)
	}
}

func TestMigrationsOrderIdempotentAndRollback(t *testing.T) {
	d := open(t)
	ms := []Migration{
		{2, "b", `CREATE TABLE b (id INTEGER PRIMARY KEY, a_id INTEGER REFERENCES a(id)); INSERT INTO b(a_id) SELECT id FROM a`},
		{1, "a", `CREATE TABLE a (id INTEGER PRIMARY KEY); INSERT INTO a(id) VALUES (1)`},
	}
	if err := Migrate(d, "m", ms); err != nil {
		t.Fatal(err)
	}
	if count(t, d, "SELECT COUNT(*) FROM b") != 1 {
		t.Fatal("ordre non respecté")
	}
	if err := Migrate(d, "m", ms); err != nil {
		t.Fatal(err)
	}
	if count(t, d, "SELECT COUNT(*) FROM b") != 1 || count(t, d, "SELECT COUNT(*) FROM schema_migrations") != 2 {
		t.Fatal("rejeu non idempotent")
	}

	bad := []Migration{{3, "bad", `CREATE TABLE c (id INTEGER); INSERT INTO c VALUES (1); INSERT INTO nope VALUES (1)`}}
	if err := Migrate(d, "m", append(ms, bad...)); err == nil {
		t.Fatal("erreur attendue")
	}
	if count(t, d, "SELECT COUNT(*) FROM sqlite_master WHERE name = 'c'") != 0 {
		t.Fatal("migration en échec non annulée")
	}
	if count(t, d, "SELECT COUNT(*) FROM schema_migrations WHERE version = 3") != 0 {
		t.Fatal("version enregistrée malgré l'échec")
	}
}

func TestLoadFS(t *testing.T) {
	fsys := fstest.MapFS{
		"migrations/002_b.sql": {Data: []byte("SELECT 2;")},
		"migrations/001_a.sql": {Data: []byte("SELECT 1;")},
		"migrations/notes.txt": {Data: []byte("x")},
	}
	ms, err := LoadFS(fsys, "migrations")
	if err != nil || len(ms) != 2 || ms[0].Version != 1 || ms[1].Name != "b" {
		t.Fatalf("%v %v", ms, err)
	}
}
