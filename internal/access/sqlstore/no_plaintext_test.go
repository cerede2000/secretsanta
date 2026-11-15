package sqlstore

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"secretsanta/internal/access/token"
)

// Après l'émission de jetons et de codes, aucune colonne d'aucune table ne
// contient un secret en clair, sous aucune de ses formes : jeton, code, forme
// normalisée, minuscules, octets bruts du jeton ou leur hexadécimal. Les
// fichiers de la base (y compris le journal WAL) sont aussi parcourus.
func TestNoPlaintextInDatabase(t *testing.T) {
	dir := t.TempDir()
	d := openMigrated(t, dir)
	st := New(d)
	svc := newService(t, st)
	ctx := context.Background()

	var needles []string
	for _, g := range []string{"g1", "g2"} {
		for _, p := range []string{"alice", "bob", "chloe"} {
			// Une émission remplacée puis la courante : ni l'une ni l'autre
			// ne doit subsister en clair.
			for i := 0; i < 2; i++ {
				tok, err := svc.IssueLink(ctx, g, p)
				if err != nil {
					t.Fatal(err)
				}
				code, err := svc.IssueCode(ctx, g, p)
				if err != nil {
					t.Fatal(err)
				}
				raw, err := base64.RawURLEncoding.DecodeString(tok)
				if err != nil {
					t.Fatal(err)
				}
				needles = append(needles,
					tok, code, token.NormalizeCode(code), strings.ToLower(code),
					code[:4]+"-"+code[4:], string(raw), hex.EncodeToString(raw),
					base64.StdEncoding.EncodeToString(raw))
			}
		}
	}
	// Le magasin conserve bien des accès : le test parcourt des données
	// réelles et non une table vide.
	if n := countRows(t, d, ``); n != 12 {
		t.Fatalf("%d accès conservés, attendu 12", n)
	}

	tables := listTables(t, d)
	if len(tables) < 2 {
		t.Fatalf("tables parcourues : %v", tables)
	}
	cells := 0
	for _, table := range tables {
		cells += scanTable(t, d, table, needles)
	}
	if cells < 12*4 {
		t.Fatalf("seulement %d cellules parcourues", cells)
	}

	// Fichiers bruts : base, WAL et mémoire partagée.
	if err := d.Close(); err != nil {
		t.Fatal(err)
	}
	files, _ := filepath.Glob(filepath.Join(dir, "*"))
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
			if bytes.Contains(b, []byte(n)) || bytes.Contains(lower, bytes.ToLower([]byte(n))) {
				t.Fatalf("secret en clair %q dans le fichier %s", n, filepath.Base(f))
			}
		}
	}
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

// scanTable lit toutes les cellules de table et échoue si l'une contient un
// des secrets (comparaison insensible à la casse). Elle renvoie le nombre de
// cellules lues.
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
			var s string
			switch x := v.(type) {
			case nil:
				continue
			case []byte:
				s = string(x)
			case string:
				s = x
			default:
				s = fmt.Sprint(x)
			}
			for _, n := range needles {
				if strings.Contains(s, n) || strings.Contains(strings.ToLower(s), strings.ToLower(n)) {
					t.Fatalf("secret en clair %q dans %s.%s", n, table, cols[i])
				}
			}
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return cells
}
