package groups

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"secretsanta/internal/config"
	"secretsanta/internal/db"
	"secretsanta/internal/shared/clock"
)

// F2.AC4 : le secret d'administration n'est renvoyé qu'une fois, à la
// création, et n'apparaît jamais en clair en base.

func TestAdminSecretReturnedOnceAndDistinct(t *testing.T) {
	e := newEnv(t)
	a := mustCreate(t, e, validInput("famille-a"))
	b := mustCreate(t, e, validInput("famille-b"))
	if len(a.AdminSecret) < 22 || a.AdminSecret == b.AdminSecret {
		t.Fatalf("secrets faibles ou identiques : %q %q", a.AdminSecret, b.AdminSecret)
	}
	raw, err := base64.RawURLEncoding.DecodeString(a.AdminSecret)
	if err != nil || len(raw) < 16 {
		t.Fatalf("secret de moins de 128 bits : %q", a.AdminSecret)
	}

	// Aucune lecture ultérieure ne renvoie le secret ni son condensé.
	ctx := context.Background()
	g1, _ := e.svc.ByID(ctx, a.Group.ID)
	g2, _ := e.svc.BySlug(ctx, "famille-a")
	pv, _ := e.svc.PublicPage(ctx, "famille-a")
	sum := sha256.Sum256([]byte(a.AdminSecret))
	for _, v := range []any{g1, g2, pv, a.Group} {
		s := fmt.Sprintf("%+v", v)
		j, _ := json.Marshal(v)
		for _, needle := range []string{a.AdminSecret, hex.EncodeToString(sum[:])} {
			if strings.Contains(s, needle) || strings.Contains(string(j), needle) {
				t.Fatalf("lecture ultérieure divulgue le secret : %s", s)
			}
		}
	}

	// Le modèle Group ne porte ni secret ni condensé.
	gt := reflect.TypeOf(Group{})
	for i := 0; i < gt.NumField(); i++ {
		n := strings.ToLower(gt.Field(i).Name)
		if strings.Contains(n, "secret") || strings.Contains(n, "hash") {
			t.Fatalf("Group expose le champ %s", gt.Field(i).Name)
		}
	}
	// Seul Create renvoie un CreateResult.
	st := reflect.TypeOf(e.svc)
	resT := reflect.TypeOf(&CreateResult{})
	for i := 0; i < st.NumMethod(); i++ {
		m := st.Method(i)
		for j := 0; j < m.Type.NumOut(); j++ {
			if m.Type.Out(j) == resT && m.Name != "Create" {
				t.Fatalf("%s renvoie le secret d'administration", m.Name)
			}
		}
	}
}

func TestAdminSecretNeverStoredInClear(t *testing.T) {
	dir := t.TempDir()
	d, err := db.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = d.Close() }()
	ms, _ := Migrations()
	if err := db.Migrate(d, Module, ms); err != nil {
		t.Fatal(err)
	}
	s, err := NewService(d, clock.NewFake(t0), config.NewSecret(testPassword))
	if err != nil {
		t.Fatal(err)
	}
	r, err := s.Create(context.Background(), validInput("famille"))
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := base64.RawURLEncoding.DecodeString(r.AdminSecret)

	// Toutes les colonnes de toutes les tables.
	rows, err := d.Query(`SELECT name FROM sqlite_master WHERE type = 'table'`)
	if err != nil {
		t.Fatal(err)
	}
	var tables []string
	for rows.Next() {
		var n string
		_ = rows.Scan(&n)
		tables = append(tables, n)
	}
	_ = rows.Close()
	for _, tb := range tables {
		rs, err := d.Query(`SELECT * FROM "` + tb + `"`)
		if err != nil {
			t.Fatal(err)
		}
		cols, _ := rs.Columns()
		for rs.Next() {
			vals := make([]any, len(cols))
			ptrs := make([]any, len(cols))
			for i := range vals {
				ptrs[i] = &vals[i]
			}
			if err := rs.Scan(ptrs...); err != nil {
				t.Fatal(err)
			}
			for i, v := range vals {
				var b []byte
				switch x := v.(type) {
				case []byte:
					b = x
				case string:
					b = []byte(x)
				}
				if bytes.Contains(b, []byte(r.AdminSecret)) || bytes.Contains(b, raw) {
					t.Fatalf("secret en clair dans %s.%s", tb, cols[i])
				}
			}
		}
		_ = rs.Close()
	}

	// Le condensé stocké correspond bien au secret.
	var stored []byte
	if err := d.QueryRow(`SELECT admin_secret_hash FROM groups WHERE slug = 'famille'`).Scan(&stored); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256([]byte(r.AdminSecret))
	if !bytes.Equal(stored, sum[:]) {
		t.Fatal("condensé stocké inattendu")
	}

	// Ni dans les fichiers de la base (y compris le journal WAL).
	files, _ := filepath.Glob(filepath.Join(dir, "*"))
	if len(files) == 0 {
		t.Fatal("aucun fichier de base trouvé")
	}
	for _, f := range files {
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		if bytes.Contains(b, []byte(r.AdminSecret)) || bytes.Contains(b, raw) {
			t.Fatalf("secret en clair dans %s", filepath.Base(f))
		}
	}
}
