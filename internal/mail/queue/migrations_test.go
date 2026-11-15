package queue_test

import (
	"strings"
	"testing"

	"secretsanta/internal/db"
	"secretsanta/internal/mail/migrations"
)

func TestMigrationsEmbedAndReplay(t *testing.T) {
	ms, err := migrations.Migrations()
	if err != nil || len(ms) == 0 {
		t.Fatalf("migrations embarquées attendues : %v %d", err, len(ms))
	}
	for i, m := range ms {
		if m.Version != i+1 || strings.TrimSpace(m.SQL) == "" {
			t.Fatalf("ordre ou contenu invalide : %+v", m)
		}
	}
	d := openDB(t)
	if err := db.Migrate(d, migrations.Module, ms); err != nil {
		t.Fatalf("rejeu sans effet attendu : %v", err)
	}
	var n int
	if err := d.QueryRow(`SELECT COUNT(*) FROM schema_migrations WHERE module=?`, migrations.Module).Scan(&n); err != nil || n != len(ms) {
		t.Fatalf("versions suivies %d/%d (%v)", n, len(ms), err)
	}
	cols := map[string]bool{}
	rows, err := d.Query(`SELECT name FROM pragma_table_info('mail_queue')`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var c string
		_ = rows.Scan(&c)
		cols[c] = true
	}
	for _, want := range []string{"status", "attempts", "last_error", "lock_token", "locked_at", "coalesce_key"} {
		if !cols[want] {
			t.Fatalf("colonne %q absente", want)
		}
	}
}
