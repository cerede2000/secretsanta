package participants

import (
	"testing"

	"secretsanta/internal/db"
)

func TestCreatedParticipantIsNormal(t *testing.T) {
	e := newEnv(t)
	p := e.add(t, "A", "B")
	if p.Role != RoleNormal {
		t.Fatalf("rôle %q", p.Role)
	}
	got, _ := e.svc.Get(t.Context(), e.admin(), e.g1, p.ID)
	if got.Role != RoleNormal {
		t.Fatalf("relu %q", got.Role)
	}
	// Insertion SQL sans rôle.
	if _, err := e.db.Exec(`INSERT INTO participants (group_id, name, first_name, name_key, joined_at) VALUES (?, 'x', 'y', 'y x', 'z')`, e.g1); err != nil {
		t.Fatal(err)
	}
	var role string
	if err := e.db.QueryRow(`SELECT draw_role FROM participants WHERE name_key = 'y x'`).Scan(&role); err != nil || role != "normal" {
		t.Fatalf("%q %v", role, err)
	}
}

func TestPreexistingDatabaseMigratesToNormal(t *testing.T) {
	d, err := db.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = d.Close() })
	migrate(t, d, 1) // base d'avant le rôle
	g := insertGroup(t, d, "ancien")
	if _, err := d.Exec(`INSERT INTO participants (group_id, name, first_name, name_key, joined_at) VALUES (?, 'B', 'A', 'a b', 'z')`, g); err != nil {
		t.Fatal(err)
	}
	migrate(t, d, 0)
	var role, locale string
	if err := d.QueryRow(`SELECT draw_role, locale FROM participants`).Scan(&role, &locale); err != nil {
		t.Fatal(err)
	}
	if role != "normal" || locale != "" {
		t.Fatalf("%q %q", role, locale)
	}
}
