package groups

import (
	"context"
	"database/sql"
	"strings"
	"testing"
	"time"

	"secretsanta/internal/config"
	"secretsanta/internal/db"
	"secretsanta/internal/shared/clock"
	"secretsanta/internal/shared/errs"
)

const testPassword = "mot-de-passe-instance-correct"

var t0 = time.Date(2026, 11, 15, 10, 0, 0, 0, time.UTC)

func openDB(t *testing.T) *sql.DB {
	t.Helper()
	d, err := db.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = d.Close() })
	ms, err := Migrations()
	if err != nil {
		t.Fatal(err)
	}
	if len(ms) == 0 {
		t.Fatal("aucune migration embarquée")
	}
	if err := db.Migrate(d, Module, ms); err != nil {
		t.Fatal(err)
	}
	return d
}

type env struct {
	db    *sql.DB
	clock *clock.Fake
	svc   *Service
}

func newEnv(t *testing.T) *env {
	t.Helper()
	d := openDB(t)
	c := clock.NewFake(t0)
	s, err := NewService(d, c, config.NewSecret(testPassword))
	if err != nil {
		t.Fatal(err)
	}
	return &env{db: d, clock: c, svc: s}
}

func validInput(slug string) CreateInput {
	return CreateInput{InstancePassword: testPassword, ClientKey: "198.51.100.1", Name: "Famille Martin", Slug: slug}
}

func mustCreate(t *testing.T, e *env, in CreateInput) *CreateResult {
	t.Helper()
	r, err := e.svc.Create(context.Background(), in)
	if err != nil {
		t.Fatalf("création refusée : %v", err)
	}
	return r
}

// wantFrench vérifie une erreur typée de la nature attendue, au message
// français (non vide, sans texte technique anglais courant).
func wantFrench(t *testing.T, err error, kind errs.Kind) {
	t.Helper()
	if err == nil {
		t.Fatal("erreur attendue")
	}
	if !errs.Is(err, kind) {
		t.Fatalf("nature %q attendue, obtenu %q (%v)", kind, errs.KindOf(err), err)
	}
	msg := errs.UserMessage(err)
	low := strings.ToLower(msg)
	for _, en := range []string{"invalid", "error", "constraint", "unique", "sql", "must", "failed"} {
		if strings.Contains(low, en) {
			t.Fatalf("message non français ou technique : %q", msg)
		}
	}
	if msg == "" || !strings.HasSuffix(msg, ".") {
		t.Fatalf("message inattendu : %q", msg)
	}
}

func countGroups(t *testing.T, d *sql.DB) int {
	t.Helper()
	var n int
	if err := d.QueryRow(`SELECT COUNT(*) FROM groups`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func intp(v int) *int { return &v }
