package draw

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"secretsanta/internal/config"
	"secretsanta/internal/db"
	"secretsanta/internal/groups"
	"secretsanta/internal/participants"
	"secretsanta/internal/rules"
	"secretsanta/internal/shared/clock"
	"secretsanta/internal/shared/errs"
	"secretsanta/internal/shared/ports"
)

var t0 = time.Date(2026, 11, 15, 10, 0, 0, 0, time.UTC)

var ctx = context.Background()

// sqlDirectory adapte la table participants au port rules.Directory.
type sqlDirectory struct{ db *sql.DB }

func (d sqlDirectory) DrawRole(ctx context.Context, groupID, id int64) (string, bool, error) {
	var role string
	err := d.db.QueryRowContext(ctx, `SELECT draw_role FROM participants WHERE group_id = ? AND id = ?`, groupID, id).Scan(&role)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	return role, err == nil, err
}

type env struct {
	db     *sql.DB
	clock  *clock.Fake
	reg    *ports.FakeRegistrationState
	groups *groups.Service
	parts  *participants.Service
	rules  *rules.Service
	svc    *Service
	names  []string // noms et prénoms saisis, pour vérifier leur absence
}

func migrateAll(t *testing.T, d *sql.DB) {
	t.Helper()
	steps := []struct {
		module string
		load   func() ([]db.Migration, error)
	}{
		{groups.Module, groups.Migrations},
		{participants.Module, participants.Migrations},
		{rules.Module, rules.Migrations},
		{Module, Migrations},
	}
	for _, s := range steps {
		ms, err := s.load()
		if err != nil {
			t.Fatal(err)
		}
		if len(ms) == 0 {
			t.Fatalf("aucune migration pour %s", s.module)
		}
		if err := db.Migrate(d, s.module, ms); err != nil {
			t.Fatal(err)
		}
	}
}

func newEnv(t *testing.T) *env {
	t.Helper()
	d, err := db.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = d.Close() })
	migrateAll(t, d)
	c := clock.NewFake(t0)
	g, err := groups.NewService(d, c, config.NewSecret("mot-de-passe-instance"))
	if err != nil {
		t.Fatal(err)
	}
	e := &env{db: d, clock: c, reg: &ports.FakeRegistrationState{Pending: map[int64]int{}}, groups: g}
	e.parts = participants.NewService(d, c, NewState(d))
	e.rules = rules.NewService(d, sqlDirectory{db: d})
	e.svc = NewService(d, c, g, e.parts, e.rules, e.reg, Options{})
	return e
}

// group crée un groupe ; maxGifts 0 garde la valeur par défaut (2).
func (e *env) group(t *testing.T, slug string, maxGifts int, revealAt *time.Time) int64 {
	t.Helper()
	if maxGifts == 0 {
		maxGifts = 2
	}
	var reveal any
	if revealAt != nil {
		reveal = revealAt.UTC().Format(time.RFC3339)
	}
	res, err := e.db.Exec(`INSERT INTO groups (slug, name, admin_secret_hash, created_at, max_gifts_per_giver, reveal_at)
		VALUES (?, 'Famille', zeroblob(32), ?, ?, ?)`, slug, t0.Format(time.RFC3339Nano), maxGifts, reveal)
	if err != nil {
		t.Fatal(err)
	}
	id, _ := res.LastInsertId()
	return id
}

// add ajoute un participant au nom reconnaissable.
func (e *env) add(t *testing.T, gid int64, first string, role participants.DrawRole) int64 {
	t.Helper()
	name := "Zx" + first + "ovitch"
	p, err := e.parts.Create(ctx, participants.AdminActor(gid), gid, participants.Input{FirstName: first, Name: name, Role: role})
	if err != nil {
		t.Fatalf("ajout de %s : %v", first, err)
	}
	e.names = append(e.names, first, name)
	return p.ID
}

// addN ajoute n participants du rôle donné.
func (e *env) addN(t *testing.T, gid int64, prefix string, n int, role participants.DrawRole) []int64 {
	t.Helper()
	var ids []int64
	for i := 0; i < n; i++ {
		ids = append(ids, e.add(t, gid, fmt.Sprintf("%s%c", prefix, 'a'+i), role))
	}
	return ids
}

func admin(gid int64) participants.Actor { return participants.AdminActor(gid) }

func who(gid, pid int64) participants.Actor { return participants.ParticipantActor(gid, pid) }

var confirmed = Request{Confirmed: true}

func (e *env) mustDraw(t *testing.T, gid int64) *Outcome {
	t.Helper()
	o, err := e.svc.Draw(ctx, admin(gid), gid, confirmed)
	if err != nil {
		t.Fatalf("tirage refusé : %v", err)
	}
	return o
}

func (e *env) mustRedraw(t *testing.T, gid int64) *Outcome {
	t.Helper()
	o, err := e.svc.Redraw(ctx, admin(gid), gid, confirmed)
	if err != nil {
		t.Fatalf("nouveau tirage refusé : %v", err)
	}
	return o
}

type rawPair struct {
	ID         int64
	Giver      int64
	Receiver   int64
	Discovered sql.NullString
}

// rawPairs lit les paires en base (vérité de référence des tests).
func (e *env) rawPairs(t *testing.T, drawID int64) []rawPair {
	t.Helper()
	rows, err := e.db.Query(`SELECT id, giver_id, receiver_id, discovered_at FROM draw_pairs WHERE draw_id = ? ORDER BY id`, drawID)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []rawPair
	for rows.Next() {
		var p rawPair
		if err := rows.Scan(&p.ID, &p.Giver, &p.Receiver, &p.Discovered); err != nil {
			t.Fatal(err)
		}
		out = append(out, p)
	}
	return out
}

func receiversOfRaw(pairs []rawPair, giver int64) []int64 {
	var out []int64
	for _, p := range pairs {
		if p.Giver == giver {
			out = append(out, p.Receiver)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

func targetIDs(ts []Target) []int64 {
	var out []int64
	for _, x := range ts {
		out = append(out, x.ReceiverID)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

func sorted(ids []int64) []int64 {
	out := append([]int64(nil), ids...)
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

func (e *env) count(t *testing.T, q string, args ...any) int {
	t.Helper()
	var n int
	if err := e.db.QueryRow(q, args...).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// wantKind vérifie la nature d'une erreur typée et un message français sans
// nom de participant.
func (e *env) wantKind(t *testing.T, err error, kind errs.Kind) string {
	t.Helper()
	if err == nil {
		t.Fatalf("erreur %q attendue", kind)
	}
	if !errs.Is(err, kind) {
		t.Fatalf("nature %q attendue, obtenu %q (%v)", kind, errs.KindOf(err), err)
	}
	msg := errs.UserMessage(err)
	if msg == "" {
		t.Fatal("message vide")
	}
	e.noNames(t, msg)
	e.noNames(t, err.Error())
	return msg
}

// noNames vérifie qu'aucun nom ou prénom saisi n'apparaît dans s.
func (e *env) noNames(t *testing.T, s string) {
	t.Helper()
	low := strings.ToLower(s)
	for _, n := range e.names {
		if strings.Contains(low, strings.ToLower(n)) {
			t.Fatalf("un nom (%q) apparaît dans %q", n, s)
		}
	}
}

// noPairType vérifie qu'un type exposé ne contient ni Pair ni paire du moteur.
func noPairType(t *testing.T, v any) {
	t.Helper()
	seen := map[reflect.Type]bool{}
	var walk func(reflect.Type, string)
	walk = func(ty reflect.Type, path string) {
		if seen[ty] {
			return
		}
		seen[ty] = true
		if ty.Name() == "Pair" {
			t.Fatalf("%s expose un type de paire (%s)", path, ty)
		}
		switch ty.Kind() {
		case reflect.Pointer, reflect.Slice, reflect.Array, reflect.Map:
			walk(ty.Elem(), path+"[]")
		case reflect.Struct:
			for i := 0; i < ty.NumField(); i++ {
				walk(ty.Field(i).Type, path+"."+ty.Field(i).Name)
			}
		}
	}
	walk(reflect.TypeOf(v), reflect.TypeOf(v).String())
}
