package sqlstore

import (
	"bytes"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"secretsanta/internal/access/token"
	"secretsanta/internal/db"
	"secretsanta/internal/registration/invite"
	"secretsanta/internal/registration/migrations"
)

func TestMigrationsAreEmbeddedAndOrdered(t *testing.T) {
	if migrations.Module != "registration" {
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

// Les trois tables (invitation, inscriptions en attente, codes) sont créées
// avec leurs colonnes, et aucune colonne ne porte de secret en clair.
func TestMigrationsCreateRegistrationTables(t *testing.T) {
	d := openMigrated(t, t.TempDir())
	want := map[string][]string{
		"registration_invitations": {"group_id", "enabled", "token_hash", "capacity", "closes_at", "updated_at"},
		"registration_pending": {"id", "group_id", "ref", "first_name", "name", "name_key", "email", "email_key",
			"shared_address", "lang", "created_at", "expires_at"},
		"registration_codes": {"group_id", "registration_id", "code_hash", "token_hash", "issued_at", "expires_at",
			"attempts", "consumed"},
	}
	for table, cols := range want {
		got := columns(t, d, table)
		if !reflect.DeepEqual(got, cols) {
			t.Fatalf("%s : colonnes %v, attendu %v", table, got, cols)
		}
		for _, c := range got {
			if c == "token" || c == "code" || c == "invite_token" || c == "confirm_token" {
				t.Fatalf("%s : colonne de secret en clair %q", table, c)
			}
		}
	}
	// Les tables d'inscription dépendent des groupes : supprimer un groupe
	// supprime ses inscriptions.
	for table := range want {
		var n int
		if err := d.QueryRow(`SELECT COUNT(*) FROM pragma_foreign_key_list(?) WHERE "table" = 'groups' AND on_delete = 'CASCADE'`,
			table).Scan(&n); err != nil {
			t.Fatal(err)
		}
		if n != 1 {
			t.Fatalf("%s : clé étrangère vers groups en cascade absente", table)
		}
	}
}

// Sur une base vide (après les groupes), chaque préfixe s'applique dans
// l'ordre et seule la version appliquée est enregistrée.
func TestMigrationsApplyInOrderOnEmptyDatabase(t *testing.T) {
	d, err := db.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	migrateAll(t, d)
	ms, _ := migrations.Migrations()
	got := appliedVersions(t, d)
	if len(got) != len(ms) {
		t.Fatalf("versions appliquées %v pour %d migrations", got, len(ms))
	}
	for i, v := range got {
		if v != i+1 {
			t.Fatalf("versions appliquées %v", got)
		}
	}
}

// Rejouer les migrations ne change ni le schéma, ni le suivi, ni les données.
func TestMigrationsReplayWithoutEffect(t *testing.T) {
	e := newEnv(t)
	link := e.enable(t, e.g1)
	if r := e.submit(t, "noel", link.Token, submission("Léa", "Martin", "lea@example.org")); r.Outcome != invite.OutcomeCodeSent {
		t.Fatalf("issue %s", r.Outcome)
	}
	before := snapshot(t, e.db)
	ms, _ := migrations.Migrations()
	for i := 0; i < 2; i++ {
		if err := db.Migrate(e.db, migrations.Module, ms); err != nil {
			t.Fatalf("rejeu %d : %v", i+1, err)
		}
	}
	if after := snapshot(t, e.db); !reflect.DeepEqual(before, after) {
		t.Fatalf("le rejeu a modifié la base :\navant %v\naprès %v", before, after)
	}
	for _, table := range []string{"registration_invitations", "registration_pending", "registration_codes"} {
		if countRows(t, e.db, table) != 1 {
			t.Fatalf("%s : données perdues au rejeu", table)
		}
	}
}

// Les tables refusent ce qui n'a pas la forme d'une empreinte (un jeton ou un
// code en clair par exemple).
func TestMigrationsRejectPlaintextShapedSecrets(t *testing.T) {
	d := openMigrated(t, t.TempDir())
	g := insertGroup(t, d, "noel")
	tok := "AbCdEfGhIjKlMnOpQrStUv"
	for _, h := range []any{tok, []byte(tok), rawHash("x")[:31], append(rawHash("x"), 0), hex.EncodeToString(rawHash("x"))} {
		if _, err := d.Exec(`INSERT INTO registration_invitations (group_id, enabled, token_hash, updated_at) VALUES (?, 1, ?, 0)`,
			g, h); err == nil {
			t.Errorf("condensé d'invitation mal formé accepté : %v", h)
		}
	}
	if _, err := d.Exec(`INSERT INTO registration_invitations (group_id, enabled, token_hash, updated_at) VALUES (?, 1, ?, 0)`,
		g, rawHash("x")); err != nil {
		t.Fatalf("condensé valide refusé : %v", err)
	}
	h := hexHash("ok")
	for _, tc := range []struct{ codeHash, tokenHash string }{
		{"ABC234", h},
		{h, tok},
		{strings.ToUpper(h), h},
		{h[:63] + "z", h},
		{h + "0", h},
	} {
		if _, err := d.Exec(`INSERT INTO registration_codes (group_id, registration_id, code_hash, token_hash, issued_at, expires_at)
			VALUES (?, 1, ?, ?, 0, 1)`, g, tc.codeHash, tc.tokenHash); err == nil {
			t.Errorf("empreinte de code mal formée acceptée : %+v", tc)
		}
	}
}

// Après activation, renouvellement, inscriptions et renvois de code, aucune
// cellule d'aucune table ni aucun fichier de la base (WAL compris) ne
// contient un jeton d'invitation, un code ou un jeton de confirmation en
// clair, sous aucune de ses formes.
func TestNoPlaintextSecretInDatabase(t *testing.T) {
	e := newEnv(t)
	ctx := t.Context()
	var needles []string
	addToken := func(tok string) {
		raw, err := base64.RawURLEncoding.DecodeString(tok)
		if err != nil {
			t.Fatal(err)
		}
		needles = append(needles, tok, string(raw), hex.EncodeToString(raw), base64.StdEncoding.EncodeToString(raw))
	}
	addCode := func(c string) {
		needles = append(needles, c, strings.ToLower(c), token.NormalizeCode(c), c[:3]+"-"+c[3:])
	}

	people := []struct{ first, name, email string }{
		{"Léa", "Martin", "lea@example.org"},
		{"Hugo", "Bernard", "hugo@example.org"},
	}
	for _, g := range []struct {
		id   int64
		slug string
	}{{e.g1, "noel"}, {e.g2, "famille"}} {
		first := e.enable(t, g.id)
		addToken(first.Token)
		renewed, err := e.svc.Renew(ctx, adminOf(g.id), g.id)
		if err != nil {
			t.Fatal(err)
		}
		addToken(renewed.Token)
		for _, p := range people {
			r := e.submit(t, g.slug, renewed.Token, submission(p.first, p.name, p.email))
			if r.Outcome != invite.OutcomeCodeSent {
				t.Fatalf("issue %s", r.Outcome)
			}
			m := e.mail.last(t)
			addCode(m.Code)
			addToken(path.Base(m.Link))
			// Renvoi : l'ancien et le nouveau secret sont cherchés.
			e.clock.Advance(61 * time.Second)
			pend := e.pendingByRef(t, g.id, r.Ref)
			if _, err := e.codeSvc.Issue(ctx, codeRequest(g.slug, pend)); err != nil {
				t.Fatal(err)
			}
			m = e.mail.last(t)
			addCode(m.Code)
			addToken(path.Base(m.Link))
		}
	}
	if n := countRows(t, e.db, "registration_codes"); n != 4 {
		t.Fatalf("%d codes conservés, attendu 4", n)
	}
	if n := countRows(t, e.db, "registration_invitations"); n != 2 {
		t.Fatalf("%d invitations, attendu 2", n)
	}

	cells := 0
	for _, table := range listTables(t, e.db) {
		cells += scanTable(t, e.db, table, needles)
	}
	if cells < 4*8 {
		t.Fatalf("seulement %d cellules parcourues", cells)
	}
	if err := e.db.Close(); err != nil {
		t.Fatal(err)
	}
	files, _ := filepath.Glob(filepath.Join(e.dir, "*"))
	if len(files) == 0 {
		t.Fatal("aucun fichier de base trouvé")
	}
	for _, f := range files {
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		lower := bytes.ToLower(b)
		for _, n := range needles {
			if len(n) < 6 {
				t.Fatalf("aiguille trop courte : %q", n)
			}
			if bytes.Contains(b, []byte(n)) || bytes.Contains(lower, bytes.ToLower([]byte(n))) {
				t.Fatalf("secret en clair %q dans le fichier %s", n, filepath.Base(f))
			}
		}
	}
}

func columns(t *testing.T, d *sql.DB, table string) []string {
	t.Helper()
	rows, err := d.Query(`SELECT name FROM pragma_table_info(?) ORDER BY cid`, table)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			t.Fatal(err)
		}
		out = append(out, n)
	}
	return out
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

// snapshot capture le schéma, le suivi des migrations et les données de
// l'inscription.
func snapshot(t *testing.T, d *sql.DB) []string {
	t.Helper()
	var out []string
	for _, q := range []string{
		`SELECT type || '|' || name || '|' || COALESCE(sql, '') FROM sqlite_master ORDER BY type, name`,
		`SELECT module || '|' || version || '|' || name || '|' || applied_at FROM schema_migrations ORDER BY module, version`,
		`SELECT group_id || '|' || enabled || '|' || hex(token_hash) || '|' || capacity || '|' || updated_at FROM registration_invitations ORDER BY 1`,
		`SELECT id || '|' || group_id || '|' || ref || '|' || name_key || '|' || email_key || '|' || expires_at FROM registration_pending ORDER BY 1`,
		`SELECT group_id || '|' || registration_id || '|' || code_hash || '|' || token_hash || '|' || attempts FROM registration_codes ORDER BY 1`,
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

func listTables(t *testing.T, d *sql.DB) []string {
	t.Helper()
	rows, err := d.Query(`SELECT name FROM sqlite_master WHERE type = 'table' ORDER BY name`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			t.Fatal(err)
		}
		out = append(out, n)
	}
	return out
}

// scanTable lit toutes les cellules de table (texte, octets bruts et leur
// hexadécimal) et échoue si l'une contient un secret. Elle renvoie le nombre
// de cellules lues.
func scanTable(t *testing.T, d *sql.DB, table string, needles []string) int {
	t.Helper()
	rows, err := d.Query(fmt.Sprintf(`SELECT * FROM "%s"`, strings.ReplaceAll(table, `"`, `""`)))
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	cols, err := rows.Columns()
	if err != nil {
		t.Fatal(err)
	}
	cells := 0
	for rows.Next() {
		vals := make([]any, len(cols))
		ptrs := make([]any, len(cols))
		for i := range vals {
			ptrs[i] = &vals[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			t.Fatal(err)
		}
		for i, v := range vals {
			cells++
			var forms []string
			switch x := v.(type) {
			case nil:
				continue
			case []byte:
				forms = []string{string(x), hex.EncodeToString(x)}
			case string:
				forms = []string{x}
			default:
				forms = []string{fmt.Sprint(x)}
			}
			for _, s := range forms {
				for _, n := range needles {
					if strings.Contains(strings.ToLower(s), strings.ToLower(n)) {
						t.Fatalf("secret en clair %q dans %s.%s", n, table, cols[i])
					}
				}
			}
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return cells
}
