package sqlstore

import (
	"database/sql"
	"testing"

	"secretsanta/internal/access/migrations"
	"secretsanta/internal/access/token"
	"secretsanta/internal/db"
)

// testKey tient lieu de clé de signature de l'instance (SIGNING_SECRET).
var testKey = []byte("cle-de-signature-sqlstore-0123456789")

// openMigrated ouvre une base vide dans dir et lui applique les migrations
// d'accès.
func openMigrated(t testing.TB, dir string) *sql.DB {
	t.Helper()
	d, err := db.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = d.Close() })
	ms, err := migrations.Migrations()
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Migrate(d, migrations.Module, ms); err != nil {
		t.Fatal(err)
	}
	return d
}

func newStore(t testing.TB) (*Store, *sql.DB) {
	t.Helper()
	d := openMigrated(t, t.TempDir())
	return New(d), d
}

func newService(t testing.TB, st token.Store) *token.Service {
	t.Helper()
	svc, err := token.NewService(st, token.LoginCodeLength, testKey)
	if err != nil {
		t.Fatal(err)
	}
	return svc
}

func countRows(t testing.TB, d *sql.DB, where string, args ...any) int {
	t.Helper()
	var n int
	if err := d.QueryRow(`SELECT COUNT(*) FROM access_credentials `+where, args...).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// hashOf renvoie une empreinte valide (forme hexadécimale de 64 caractères)
// pour un secret de test.
func hashOf(group string, kind token.Kind, secret string) string {
	return token.Hash(testKey, group, kind, secret)
}
