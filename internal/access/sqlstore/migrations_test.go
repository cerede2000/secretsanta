package sqlstore

import (
	"database/sql"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"secretsanta/internal/access/migrations"
	"secretsanta/internal/db"
)

func TestMigrationsAreEmbeddedAndOrdered(t *testing.T) {
	if migrations.Module != "access_credentials" {
		t.Fatalf("module %q", migrations.Module)
	}
	ms, err := migrations.Migrations()
	if err != nil {
		t.Fatal(err)
	}
	if len(ms) == 0 {
		t.Fatal("aucune migration embarquée")
	}
	for i, m := range ms {
		if m.Version != i+1 {
			t.Fatalf("migration %d : version %d, attendu une suite continue depuis 1", i, m.Version)
		}
		if m.Name == "" || strings.TrimSpace(m.SQL) == "" {
			t.Fatalf("migration %d vide ou sans nom", m.Version)
		}
	}
}

// Sur une base vide, chaque préfixe de la liste s'applique dans l'ordre et la
// table de suivi n'enregistre que les versions appliquées.
func TestMigrationsApplyInOrderOnEmptyDatabase(t *testing.T) {
	ms, err := migrations.Migrations()
	if err != nil {
		t.Fatal(err)
	}
	d, err := db.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	if n := tableCount(t, d); n != 0 {
		t.Fatalf("base non vide : %d tables", n)
	}
	for i := 1; i <= len(ms); i++ {
		if err := db.Migrate(d, migrations.Module, ms[:i]); err != nil {
			t.Fatalf("préfixe %d : %v", i, err)
		}
		got := appliedVersions(t, d)
		want := make([]int, i)
		for j := range want {
			want[j] = j + 1
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("versions appliquées %v, attendu %v", got, want)
		}
	}
	assertSchema(t, d)
}

// Rejouer les migrations ne change ni le schéma, ni la table de suivi, ni les
// données.
func TestMigrationsReplayWithoutEffect(t *testing.T) {
	_, d := newStore(t)
	if _, err := d.Exec(`INSERT INTO access_credentials (group_id, participant_id, kind, token_hash) VALUES ('g', 'p', 'code', ?)`,
		hashOf("g", "code", "ABCD2345")); err != nil {
		t.Fatal(err)
	}
	before := snapshot(t, d)
	ms, _ := migrations.Migrations()
	for i := 0; i < 2; i++ {
		if err := db.Migrate(d, migrations.Module, ms); err != nil {
			t.Fatalf("rejeu %d : %v", i+1, err)
		}
	}
	if after := snapshot(t, d); !reflect.DeepEqual(before, after) {
		t.Fatalf("le rejeu a modifié la base :\navant %v\naprès %v", before, after)
	}
	assertSchema(t, d)
}

// Les contraintes d'unicité s'exercent réellement en SQL.
func TestMigrationsEnforceUniqueness(t *testing.T) {
	_, d := newStore(t)
	ins := func(group, participant, kind, hash string) error {
		_, err := d.Exec(`INSERT INTO access_credentials (group_id, participant_id, kind, token_hash) VALUES (?, ?, ?, ?)`,
			group, participant, kind, hash)
		return err
	}
	h := hashOf("g", "code", "ABCD2345")
	h2 := hashOf("g", "code", "ABCD2346")
	if err := ins("g", "alice", "code", h); err != nil {
		t.Fatal(err)
	}
	if err := ins("g", "bob", "code", h); err == nil {
		t.Fatal("(groupe, nature, empreinte) en double accepté")
	}
	if err := ins("g", "alice", "code", h2); err == nil {
		t.Fatal("(groupe, nature, participant) en double accepté")
	}
	if err := ins("g2", "bob", "code", h); err != nil {
		t.Fatalf("même empreinte dans un autre groupe refusée : %v", err)
	}
	if err := ins("g", "alice", "link", h); err != nil {
		t.Fatalf("même empreinte pour une autre nature refusée : %v", err)
	}
}

// La table refuse ce qui n'a pas la forme d'une empreinte (un secret en clair
// par exemple), une nature inconnue et des identifiants vides.
func TestMigrationsRejectMalformedRows(t *testing.T) {
	_, d := newStore(t)
	h := hashOf("g", "code", "ABCD2345")
	for _, tc := range []struct{ group, participant, kind, hash string }{
		{"g", "p", "code", "ABCD2345"},
		{"g", "p", "code", strings.ToUpper(h)},
		{"g", "p", "code", h[:63] + "z"},
		{"g", "p", "code", h + "0"},
		{"g", "p", "magic", h},
		{"", "p", "code", h},
		{"g", "", "code", h},
	} {
		if _, err := d.Exec(`INSERT INTO access_credentials (group_id, participant_id, kind, token_hash) VALUES (?, ?, ?, ?)`,
			tc.group, tc.participant, tc.kind, tc.hash); err == nil {
			t.Errorf("ligne mal formée acceptée : %+v", tc)
		}
	}
}

func tableCount(t *testing.T, d *sql.DB) int {
	t.Helper()
	var n int
	if err := d.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type = 'table'`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func appliedVersions(t *testing.T, d *sql.DB) []int {
	t.Helper()
	rows, err := d.Query(`SELECT version FROM schema_migrations WHERE module = ? ORDER BY version`, migrations.Module)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []int
	for rows.Next() {
		var v int
		if err := rows.Scan(&v); err != nil {
			t.Fatal(err)
		}
		out = append(out, v)
	}
	return out
}

// assertSchema vérifie la table des jetons et des codes et ses deux index
// uniques.
func assertSchema(t *testing.T, d *sql.DB) {
	t.Helper()
	cols := map[string]bool{}
	rows, err := d.Query(`SELECT name FROM pragma_table_info('access_credentials')`)
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			t.Fatal(err)
		}
		cols[n] = true
	}
	rows.Close()
	want := map[string]bool{"group_id": true, "participant_id": true, "kind": true, "token_hash": true}
	if !reflect.DeepEqual(cols, want) {
		t.Fatalf("colonnes %v, attendu %v", cols, want)
	}

	uniques := map[string]bool{}
	rows, err = d.Query(`SELECT name FROM pragma_index_list('access_credentials') WHERE "unique" = 1`)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			t.Fatal(err)
		}
		names = append(names, n)
	}
	rows.Close()
	for _, n := range names {
		r, err := d.Query(`SELECT name FROM pragma_index_info(?) ORDER BY seqno`, n)
		if err != nil {
			t.Fatal(err)
		}
		var cs []string
		for r.Next() {
			var c string
			if err := r.Scan(&c); err != nil {
				t.Fatal(err)
			}
			cs = append(cs, c)
		}
		r.Close()
		uniques[strings.Join(cs, ",")] = true
	}
	for _, key := range []string{"group_id,kind,token_hash", "group_id,kind,participant_id"} {
		if !uniques[key] {
			t.Fatalf("unicité (%s) absente ; index uniques : %v", key, uniques)
		}
	}
}

// snapshot capture le schéma, la table de suivi et les données d'accès.
func snapshot(t *testing.T, d *sql.DB) []string {
	t.Helper()
	var out []string
	for _, q := range []string{
		`SELECT type || '|' || name || '|' || COALESCE(sql, '') FROM sqlite_master ORDER BY type, name`,
		`SELECT module || '|' || version || '|' || name || '|' || applied_at FROM schema_migrations ORDER BY module, version`,
		`SELECT group_id || '|' || participant_id || '|' || kind || '|' || token_hash FROM access_credentials ORDER BY 1`,
	} {
		rows, err := d.Query(q)
		if err != nil {
			t.Fatal(err)
		}
		for rows.Next() {
			var s string
			if err := rows.Scan(&s); err != nil {
				t.Fatal(err)
			}
			out = append(out, s)
		}
		rows.Close()
		out = append(out, fmt.Sprintf("--- %d", len(out)))
	}
	return out
}
