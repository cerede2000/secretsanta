package reveal

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"secretsanta/internal/config"
	"secretsanta/internal/db"
	"secretsanta/internal/draw"
	"secretsanta/internal/groups"
	"secretsanta/internal/participants"
	"secretsanta/internal/rules"
	"secretsanta/internal/shared/clock"
	"secretsanta/internal/shared/errs"
	"secretsanta/internal/shared/ports"
	"secretsanta/internal/shared/scheduler"
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

// fakeMailer mémorise les avis mis en file.
type fakeMailer struct {
	mu   sync.Mutex
	sent []ReadyNotice
	err  error
}

func (m *fakeMailer) EnqueueReady(_ context.Context, n ReadyNotice) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.err != nil {
		return m.err
	}
	m.sent = append(m.sent, n)
	return nil
}

func (m *fakeMailer) notices() []ReadyNotice {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]ReadyNotice(nil), m.sent...)
}

type env struct {
	db     *sql.DB
	clock  *clock.Fake
	sched  *scheduler.Scheduler
	mail   *fakeMailer
	groups *groups.Service
	parts  *participants.Service
	draw   *draw.Service
	svc    *Service
	names  []string
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
		{draw.Module, draw.Migrations},
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
	e := &env{db: d, clock: c, sched: scheduler.New(c), mail: &fakeMailer{}, groups: g}
	e.parts = participants.NewService(d, c, draw.NewState(d))
	r := rules.NewService(d, sqlDirectory{db: d})
	e.draw = draw.NewService(d, c, g, e.parts, r, &ports.FakeRegistrationState{Pending: map[int64]int{}},
		draw.Options{Gate: NewGate(d, c)})
	e.svc = NewService(d, c, e.sched, e.draw, e.mail, Options{})
	return e
}

// group crée un groupe, avec ou sans date de révélation.
func (e *env) group(t *testing.T, slug string, revealAt *time.Time) int64 {
	t.Helper()
	var reveal any
	if revealAt != nil {
		reveal = revealAt.UTC().Format(time.RFC3339)
	}
	res, err := e.db.Exec(`INSERT INTO groups (slug, name, admin_secret_hash, created_at, max_gifts_per_giver, reveal_at)
		VALUES (?, 'Famille', zeroblob(32), ?, 2, ?)`, slug, t0.Format(time.RFC3339Nano), reveal)
	if err != nil {
		t.Fatal(err)
	}
	id, _ := res.LastInsertId()
	return id
}

func (e *env) add(t *testing.T, gid int64, first string, role participants.DrawRole) int64 {
	t.Helper()
	name := "Zq" + first + "enko"
	p, err := e.parts.Create(ctx, participants.AdminActor(gid), gid, participants.Input{FirstName: first, Name: name, Role: role})
	if err != nil {
		t.Fatalf("ajout de %s : %v", first, err)
	}
	e.names = append(e.names, first, name)
	return p.ID
}

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

// mustDraw lance le tirage puis prévient la révélation, comme l'orchestrateur.
func (e *env) mustDraw(t *testing.T, gid int64) *draw.Outcome {
	t.Helper()
	o, err := e.draw.Draw(ctx, admin(gid), gid, draw.Request{Confirmed: true})
	if err != nil {
		t.Fatalf("tirage refusé : %v", err)
	}
	if err := e.svc.OnDrawChanged(ctx, gid); err != nil {
		t.Fatalf("révélation après tirage : %v", err)
	}
	return o
}

func (e *env) mustRedraw(t *testing.T, gid int64) *draw.Outcome {
	t.Helper()
	o, err := e.draw.Redraw(ctx, admin(gid), gid, draw.Request{Confirmed: true})
	if err != nil {
		t.Fatalf("nouveau tirage refusé : %v", err)
	}
	if err := e.svc.OnDrawChanged(ctx, gid); err != nil {
		t.Fatalf("révélation après nouveau tirage : %v", err)
	}
	return o
}

func (e *env) tick(t *testing.T) int {
	t.Helper()
	n, err := e.sched.Tick(ctx)
	if err != nil {
		t.Fatalf("tâche planifiée en échec : %v", err)
	}
	return n
}

type rawPair struct {
	ID         int64
	Giver      int64
	Receiver   int64
	Discovered sql.NullString
}

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

func targetIDs(ts []draw.Target) []int64 {
	var out []int64
	for _, x := range ts {
		out = append(out, x.ReceiverID)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

// targetOpenings lit les instants d'ouverture par cible (pair_id → instant).
func (e *env) targetOpenings(t *testing.T, drawID int64) map[int64]string {
	t.Helper()
	rows, err := e.db.Query(`SELECT pair_id, opened_at FROM reveal_targets WHERE draw_id = ?`, drawID)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	out := map[int64]string{}
	for rows.Next() {
		var id int64
		var at string
		if err := rows.Scan(&id, &at); err != nil {
			t.Fatal(err)
		}
		out[id] = at
	}
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

// wantKind vérifie la nature d'une erreur et un message français sans nom.
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

func (e *env) noNames(t *testing.T, s string) {
	t.Helper()
	low := strings.ToLower(s)
	for _, n := range e.names {
		if strings.Contains(low, strings.ToLower(n)) {
			t.Fatalf("un nom (%q) apparaît dans %q", n, s)
		}
	}
}

// wantFrench vérifie qu'un message est rédigé en français.
func wantFrench(t *testing.T, msg string) {
	t.Helper()
	low := strings.ToLower(msg)
	for _, w := range []string{"la ", "le ", "une ", "est ", "de "} {
		if strings.Contains(low, w) {
			return
		}
	}
	t.Fatalf("message non français : %q", msg)
}

// wantClosed vérifie que le serveur ne renvoie aucune cible à personne.
func (e *env) wantClosed(t *testing.T, gid int64, ids []int64) {
	t.Helper()
	for _, id := range ids {
		mine, err := e.draw.MyTargets(ctx, who(gid, id), gid)
		if err != nil {
			t.Fatal(err)
		}
		if mine.Ready || len(mine.Targets) != 0 {
			t.Fatalf("participant %d : cible renvoyée avant la révélation : %+v", id, mine)
		}
		hist, err := e.draw.History(ctx, who(gid, id), gid)
		if err != nil {
			t.Fatal(err)
		}
		for _, h := range hist {
			if !h.Archived && len(h.MyTargets) != 0 {
				t.Fatalf("participant %d : l'historique divulgue la cible avant la révélation", id)
			}
		}
	}
}

// wantOpen vérifie que chacun reçoit ses cibles et seulement les siennes.
func (e *env) wantOpen(t *testing.T, gid int64, drawID int64, ids []int64) {
	t.Helper()
	raw := e.rawPairs(t, drawID)
	for _, id := range ids {
		mine, err := e.draw.MyTargets(ctx, who(gid, id), gid)
		if err != nil {
			t.Fatal(err)
		}
		if !mine.Ready {
			t.Fatalf("participant %d : révélation fermée", id)
		}
		if got, want := targetIDs(mine.Targets), receiversOfRaw(raw, id); !reflect.DeepEqual(got, want) {
			t.Fatalf("participant %d : %v au lieu de %v", id, got, want)
		}
	}
}

// noPairType vérifie qu'un type exposé ne contient aucun type de paire ni de
// cible.
func noPairType(t *testing.T, v any) {
	t.Helper()
	seen := map[reflect.Type]bool{}
	var walk func(reflect.Type, string)
	walk = func(ty reflect.Type, path string) {
		if seen[ty] {
			return
		}
		seen[ty] = true
		if ty.Name() == "Pair" || ty.Name() == "Target" {
			t.Fatalf("%s expose un type de paire (%s)", path, ty)
		}
		switch ty.Kind() {
		case reflect.Pointer, reflect.Slice, reflect.Array, reflect.Map:
			walk(ty.Elem(), path+"[]")
		case reflect.Struct:
			for i := 0; i < ty.NumField(); i++ {
				f := ty.Field(i)
				if strings.Contains(strings.ToLower(f.Name), "receiver") || strings.Contains(strings.ToLower(f.Name), "giver") {
					t.Fatalf("%s.%s désigne une personne d'une paire", path, f.Name)
				}
				walk(f.Type, path+"."+f.Name)
			}
		}
	}
	walk(reflect.TypeOf(v), reflect.TypeOf(v).String())
}

func ptr(t time.Time) *time.Time { return &t }
