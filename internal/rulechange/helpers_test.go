package rulechange

import (
	"database/sql"
	"strings"
	"testing"
	"time"

	"secretsanta/internal/db"
	"secretsanta/internal/groups"
	"secretsanta/internal/participants"
	"secretsanta/internal/rules"
	"secretsanta/internal/shared/clock"
	"secretsanta/internal/shared/ports"
)

var t0 = time.Date(2026, 11, 15, 10, 0, 0, 0, time.UTC)

type env struct {
	db     *sql.DB
	draw   *ports.FakeDrawState
	repair *ports.FakeRuleChangeRepair
	svc    *Service
	parts  *participants.Service
	gid    int64
	ids    []int64
}

func newEnv(t *testing.T, drawDone bool) *env {
	t.Helper()
	d, err := db.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = d.Close() })
	gm, _ := groups.Migrations()
	pm, _ := participants.Migrations()
	rm, _ := rules.Migrations()
	cm, err := Migrations()
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range []struct {
		name string
		ms   []db.Migration
	}{{groups.Module, gm}, {participants.Module, pm}, {rules.Module, rm}, {Module, cm}} {
		if err := db.Migrate(d, m.name, m.ms); err != nil {
			t.Fatal(err)
		}
	}
	res, err := d.Exec(`INSERT INTO groups(name, slug, admin_secret_hash, created_at) VALUES ('G','groupe-un', zeroblob(32), '2026-01-01T00:00:00Z')`)
	if err != nil {
		t.Fatal(err)
	}
	gid, _ := res.LastInsertId()
	c := clock.NewFake(t0)
	draw := &ports.FakeDrawState{Done: map[int64]bool{gid: drawDone}}
	repair := &ports.FakeRuleChangeRepair{}
	e := &env{db: d, draw: draw, repair: repair, gid: gid,
		svc:   NewService(d, c, draw, repair),
		parts: participants.NewService(d, c, draw)}
	for _, n := range []string{"Alice", "Bruno", "Chloe", "David"} {
		p, err := e.parts.Create(t.Context(), participants.AdminActor(gid), gid, participants.Input{FirstName: n, Name: n + "X"})
		if err != nil {
			t.Fatal(err)
		}
		e.ids = append(e.ids, p.ID)
	}
	return e
}

func (e *env) count(t *testing.T, q string, args ...any) int {
	t.Helper()
	var n int
	if err := e.db.QueryRow(q, args...).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func (e *env) role(t *testing.T, id int64) string {
	t.Helper()
	var r string
	if err := e.db.QueryRow(`SELECT draw_role FROM participants WHERE id=?`, id).Scan(&r); err != nil {
		t.Fatal(err)
	}
	return r
}

// noNames vérifie qu'un texte ne contient aucun prénom ni nom du jeu d'essai.
func noNames(t *testing.T, s string) {
	t.Helper()
	for _, n := range []string{"Alice", "Bruno", "Chloe", "David"} {
		if strings.Contains(s, n) {
			t.Fatalf("le texte contient le nom %q : %s", n, s)
		}
	}
}
