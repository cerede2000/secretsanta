package adminops_test

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"testing"

	"secretsanta/internal/adminops"
	"secretsanta/internal/app"
	"secretsanta/internal/config"
	"secretsanta/internal/db"
	"secretsanta/internal/groups"
	"secretsanta/internal/participants"
)

// restore écrit la sauvegarde comme fichier de base d'un DATA_DIR neuf (la
// procédure de restauration) et l'ouvre comme l'application.
func restore(t *testing.T, backup []byte) *fixture {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, db.FileName), backup, 0o600); err != nil {
		t.Fatal(err)
	}
	d, err := db.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = d.Close() })
	return &fixture{db: d}
}

// F10b.AC4 : la sauvegarde est une copie SQLite cohérente et restaurable, et
// l'export puis l'import JSON reproduit les participants.
func TestBackupIsConsistentAndRestorable(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	g1, secret1 := f.group(t, "famille", "Famille", "2026-12-24")
	g2, _ := f.group(t, "amis", "Amis", "")
	a := f.person(t, g1, "Élodie", "Martin", "elodie@exemple.fr", "")
	b := f.person(t, g1, "Paul", "Martin", "", participants.RoleOffrePlus)
	c := f.person(t, g1, "Zoé", "Durand", "", participants.RoleReceptionSeule)
	f.household(t, g1, "Les Martin", a, b)
	f.exec(t, `INSERT INTO manual_exclusions(group_id, giver_id, receiver_id) VALUES (?, ?, ?)`, g1, c, a)
	f.seedDrawAndLetters(t, g1, []int64{a, b, c})
	f.seedTraces(t, g1, a)
	x := f.person(t, g2, "Xavier", "Autre", "x@exemple.fr", "")
	y := f.person(t, g2, "Yasmine", "Autre", "", "")
	f.household(t, g2, "Autres", x, y)
	f.seedDrawAndLetters(t, g2, []int64{x, y})
	f.seedTraces(t, g2, x)

	if last, err := f.ops.LastBackup(ctx, g1); err != nil || last != nil {
		t.Fatalf("dernière sauvegarde avant : %v %v", last, err)
	}
	var buf bytes.Buffer
	if err := f.ops.Backup(ctx, g1, &buf); err != nil {
		t.Fatalf("sauvegarde : %v", err)
	}
	if !bytes.HasPrefix(buf.Bytes(), []byte("SQLite format 3\x00")) {
		t.Fatal("la sauvegarde n'est pas un fichier SQLite")
	}
	if last, err := f.ops.LastBackup(ctx, g1); err != nil || last == nil || !last.Equal(t0) {
		t.Errorf("dernière sauvegarde : %v %v", last, err)
	}

	r := restore(t, buf.Bytes())
	var integrity string
	if err := r.db.QueryRow(`PRAGMA integrity_check`).Scan(&integrity); err != nil || integrity != "ok" {
		t.Fatalf("intégrité : %q %v", integrity, err)
	}
	// Le schéma est complet : rejouer toutes les migrations ne change rien.
	if err := app.Migrate(r.db); err != nil {
		t.Fatalf("migrations sur la copie : %v", err)
	}
	// Le groupe est restauré à l'identique : participants, foyers,
	// exclusions, tirage et lettres ; le secret d'administration fonctionne.
	if got, want := roster(t, r.db, g1), roster(t, f.db, g1); !reflect.DeepEqual(got, want) {
		t.Errorf("participants restaurés : %v / %v", got, want)
	}
	if got, want := households(t, r.db, g1), households(t, f.db, g1); !reflect.DeepEqual(got, want) {
		t.Errorf("foyers restaurés : %v / %v", got, want)
	}
	if got, want := namedPairs(t, r.db, "manual_exclusions", g1), namedPairs(t, f.db, "manual_exclusions", g1); !reflect.DeepEqual(got, want) {
		t.Errorf("exclusions restaurées : %v / %v", got, want)
	}
	if n := r.count(t, `SELECT COUNT(*) FROM draw_pairs p JOIN draws d ON d.id = p.draw_id WHERE d.group_id = ?`, g1); n != 3 {
		t.Errorf("%d paires restaurées, 3 attendues", n)
	}
	if n := r.count(t, `SELECT COUNT(*) FROM letter_ideas i JOIN letters l ON l.id = i.letter_id WHERE l.group_id = ?`, g1); n != 3 {
		t.Errorf("%d idées restaurées, 3 attendues", n)
	}
	if n := r.count(t, `SELECT COUNT(*) FROM access_credentials WHERE group_id = ?`, strconv.FormatInt(g1, 10)); n != 1 {
		t.Errorf("liens personnels non restaurés : %d", n)
	}
	gs, err := groups.NewService(r.db, f.clock, config.NewSecret(instancePass))
	if err != nil {
		t.Fatal(err)
	}
	if id, err := gs.VerifyAdminSecret(ctx, "famille", secret1); err != nil || id != g1 {
		t.Errorf("secret d'administration sur la copie : %d %v", id, err)
	}
	// Rien de l'autre groupe, aucune session, aucun lien de connexion.
	for table, n := range groupRowCounts(t, r.db, g2) {
		if n != 0 {
			t.Errorf("la copie contient %d lignes de l'autre groupe dans %s", n, table)
		}
	}
	for _, q := range []string{
		`SELECT COUNT(*) FROM groups`, `SELECT COUNT(*) FROM websec_sessions`, `SELECT COUNT(*) FROM magic_links`,
		`SELECT COUNT(*) FROM household_members m LEFT JOIN households h ON h.id = m.household_id WHERE h.id IS NULL`,
		`SELECT COUNT(*) FROM draw_pairs WHERE giver_id IN (?, ?)`,
	} {
		want := 0
		if q == `SELECT COUNT(*) FROM groups` {
			want = 1
		}
		if n := r.count(t, q, x, y); n != want {
			t.Errorf("%s : %d, %d attendu", q, n, want)
		}
	}
	// La base d'origine n'a pas bougé.
	if n := f.count(t, `SELECT COUNT(*) FROM groups`); n != 2 {
		t.Errorf("la base d'origine a %d groupes", n)
	}
	if n := f.count(t, `SELECT COUNT(*) FROM websec_sessions`); n != 2 {
		t.Errorf("la base d'origine a %d sessions", n)
	}

	// Export JSON depuis la copie restaurée puis import dans un groupe neuf :
	// les participants sont reproduits.
	ops, err := adminops.NewService(r.db, f.clock, adminops.Options{})
	if err != nil {
		t.Fatal(err)
	}
	var js bytes.Buffer
	if err := ops.ExportJSON(ctx, g1, &js); err != nil {
		t.Fatal(err)
	}
	g3, _ := f.group(t, "famille-bis", "Famille bis", "")
	if _, err := f.ops.Import(ctx, g3, &js); err != nil {
		t.Fatal(err)
	}
	if got, want := roster(t, f.db, g3), roster(t, f.db, g1); !reflect.DeepEqual(got, want) {
		t.Errorf("export puis import : %v / %v", got, want)
	}
}

// Toute table sans group_id doit dépendre d'une autre par clé étrangère (la
// réduction de la copie et la purge passent par la cascade) ; seules les
// tables globales connues y échappent. Une nouvelle table hors de cette règle
// ferait fuir des données d'autres groupes dans une sauvegarde.
func TestEveryTableIsScopedToAGroup(t *testing.T) {
	f := newFixture(t)
	global := map[string]bool{"groups": true, "schema_migrations": true}
	rows, err := f.db.Query(`SELECT m.name,
		EXISTS (SELECT 1 FROM pragma_table_info(m.name) c WHERE c.name = 'group_id'),
		(SELECT COUNT(*) FROM pragma_foreign_key_list(m.name) k WHERE k.on_delete = 'CASCADE')
		FROM sqlite_master m WHERE m.type = 'table' AND m.name NOT LIKE 'sqlite_%'`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	n := 0
	for rows.Next() {
		var name string
		var hasGroup bool
		var cascades int
		if err := rows.Scan(&name, &hasGroup, &cascades); err != nil {
			t.Fatal(err)
		}
		n++
		if !hasGroup && cascades == 0 && !global[name] {
			t.Errorf("table %s : ni group_id ni clé étrangère en cascade", name)
		}
	}
	if n < 20 {
		t.Fatalf("seulement %d tables : registre incomplet ?", n)
	}
}
