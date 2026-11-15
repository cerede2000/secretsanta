package audit

import (
	"context"
	"database/sql"
	"reflect"
	"strings"
	"testing"
	"time"

	"secretsanta/internal/db"
	"secretsanta/internal/groups"
	"secretsanta/internal/participants"
	"secretsanta/internal/shared/clock"
)

var t0 = time.Date(2026, 11, 15, 10, 0, 0, 0, time.UTC)

func open(t *testing.T) *sql.DB {
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
	return d
}

func migrate(t *testing.T, d *sql.DB) {
	t.Helper()
	ms, err := Migrations()
	if err != nil {
		t.Fatal(err)
	}
	if len(ms) == 0 {
		t.Fatal("aucune migration")
	}
	if err := db.Migrate(d, Module, ms); err != nil {
		t.Fatal(err)
	}
}

func addGroup(t *testing.T, d *sql.DB, slug string) int64 {
	t.Helper()
	res, err := d.Exec(`INSERT INTO groups (slug, name, admin_secret_hash, created_at) VALUES (?, 'Famille', zeroblob(32), ?)`,
		slug, t0.Format(time.RFC3339Nano))
	if err != nil {
		t.Fatal(err)
	}
	id, _ := res.LastInsertId()
	return id
}

func TestMigrationsCreateTheJournalAndReplayWithoutEffect(t *testing.T) {
	d := open(t)
	migrate(t, d)
	g := addGroup(t, d, "noel")
	s := New(d, clock.NewFake(t0))
	if err := s.Record(context.Background(), participants.AdminActor(g), g, ActionLinkHandedOver, 7); err != nil {
		t.Fatal(err)
	}
	migrate(t, d) // rejouer : sans effet, sans perte
	got, err := s.List(context.Background(), participants.AdminActor(g), g, 10)
	if err != nil || len(got) != 1 || got[0].Action != ActionLinkHandedOver || got[0].ParticipantID != 7 {
		t.Fatalf("journal après rejeu : %+v, %v", got, err)
	}
	var n int
	if err := d.QueryRow(`SELECT COUNT(*) FROM schema_migrations WHERE module = ?`, Module).Scan(&n); err != nil || n != len(mustMigrations(t)) {
		t.Fatalf("versions suivies : %d, %v", n, err)
	}
}

func mustMigrations(t *testing.T) []db.Migration {
	t.Helper()
	ms, err := Migrations()
	if err != nil {
		t.Fatal(err)
	}
	return ms
}

// Le journal ne stocke ni paire ni jeton : aucune colonne ne peut en porter.
func TestJournalSchemaHasNoRoomForPairsOrTokens(t *testing.T) {
	d := open(t)
	migrate(t, d)
	rows, err := d.Query(`SELECT name, type FROM pragma_table_info('admin_audit')`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	allowed := map[string]string{"id": "INTEGER", "group_id": "INTEGER", "action": "TEXT", "participant_id": "INTEGER", "at": "INTEGER"}
	seen := 0
	for rows.Next() {
		var name, typ string
		if err := rows.Scan(&name, &typ); err != nil {
			t.Fatal(err)
		}
		if allowed[name] != typ {
			t.Errorf("colonne inattendue %s %s", name, typ)
		}
		seen++
	}
	if seen != len(allowed) {
		t.Fatalf("colonnes : %d", seen)
	}
	if reflect.TypeOf(Entry{}).NumField() != 4 {
		t.Fatal("Entry ne doit porter que id, action, personne et instant")
	}
}

func TestRecordRefusesFreeTextAndOtherGroups(t *testing.T) {
	d := open(t)
	migrate(t, d)
	g1, g2 := addGroup(t, d, "groupe-un"), addGroup(t, d, "groupe-deux")
	s := New(d, clock.NewFake(t0))
	ctx := context.Background()
	for _, bad := range []string{"", "Lien remis", "lien-remis", "token=abc", strings.Repeat("a", 65), "a b"} {
		if err := s.Record(ctx, participants.AdminActor(g1), g1, bad, 0); err == nil {
			t.Errorf("l'action %q aurait dû être refusée", bad)
		}
	}
	if err := s.Record(ctx, participants.AdminActor(g1), g2, "lien_remis", 0); err == nil {
		t.Error("un administrateur ne consigne pas dans un autre groupe")
	}
	if _, err := s.List(ctx, participants.AdminActor(g1), g2, 10); err == nil {
		t.Error("un administrateur ne lit pas le journal d'un autre groupe")
	}
	if _, err := s.List(ctx, participants.Actor{}, g1, 10); err == nil {
		t.Error("un anonyme ne lit pas le journal")
	}
	if _, err := d.Exec(`INSERT INTO admin_audit (group_id, action, at) VALUES (?, 'Pas valide', 1)`, g1); err == nil {
		t.Error("la base doit refuser un code d'action libre")
	}
}
