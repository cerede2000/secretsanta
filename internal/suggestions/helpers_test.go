package suggestions

import (
	"context"
	"database/sql"
	"errors"
	"sync"
	"testing"
	"time"

	"secretsanta/internal/db"
	"secretsanta/internal/draw"
	"secretsanta/internal/groups"
	"secretsanta/internal/letter"
	"secretsanta/internal/participants"
	"secretsanta/internal/shared/clock"
	"secretsanta/internal/shared/errs"
	"secretsanta/internal/sources"
)

var ctx = context.Background()

var t0 = time.Date(2026, 11, 15, 10, 0, 0, 0, time.UTC)

type fakeTargets struct{ by map[int64][]int64 } // giver -> receivers

func (f *fakeTargets) MyTargets(_ context.Context, a participants.Actor, _ int64) (*draw.MyTargets, error) {
	out := &draw.MyTargets{Done: true, Ready: true}
	for i, r := range f.by[a.ParticipantID] {
		out.Targets = append(out.Targets, draw.Target{PairID: int64(i + 1), ReceiverID: r})
	}
	return out, nil
}

type fakeGroups struct{ g groups.Group }

func (f *fakeGroups) ByID(context.Context, int64) (*groups.Group, error) { g := f.g; return &g, nil }

type fakeLetters struct{ by map[int64]*letter.Letter }

func (f *fakeLetters) Get(_ context.Context, _, id int64) (*letter.Letter, error) {
	if l, ok := f.by[id]; ok {
		return l, nil
	}
	return &letter.Letter{}, nil
}

type fakePeople struct{ list []participants.Participant }

func (f *fakePeople) List(context.Context, participants.Actor, int64) ([]participants.Participant, error) {
	return f.list, nil
}

// fakeSearch enregistre les requêtes et sert une liste fixe.
type fakeSearch struct {
	mu      sync.Mutex
	queries []sources.Query
	res     []sources.Suggestion
	err     error
}

func (f *fakeSearch) Search(_ context.Context, q sources.Query) ([]sources.Suggestion, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.queries = append(f.queries, q)
	return f.res, f.err
}

func (f *fakeSearch) calls() int { f.mu.Lock(); defer f.mu.Unlock(); return len(f.queries) }

type fakeImages struct {
	mu     sync.Mutex
	copied []string
	fail   bool
}

func (f *fakeImages) Copy(_ context.Context, raw string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.copied = append(f.copied, raw)
	if f.fail {
		return "", errors.New("refusée")
	}
	return "/images/abc.png", nil
}

type env struct {
	db      *sql.DB
	clock   *clock.Fake
	groups  *fakeGroups
	targets *fakeTargets
	letters *fakeLetters
	people  *fakePeople
	search  *fakeSearch
	images  *fakeImages
	svc     *Service
	gid     int64
}

// Participants : 1 Alice (tireuse), 2 Bob (tiré 1), 3 Chloé (tirée 2),
// 4 Dan (réception seule, sans cible).
func newEnv(t *testing.T, opt Options) *env {
	t.Helper()
	d, err := db.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = d.Close() })
	for _, m := range []struct {
		mod  string
		load func() ([]db.Migration, error)
	}{{groups.Module, groups.Migrations}, {participants.Module, participants.Migrations}, {Module, Migrations}} {
		ms, err := m.load()
		if err != nil {
			t.Fatal(err)
		}
		if err := db.Migrate(d, m.mod, ms); err != nil {
			t.Fatal(err)
		}
	}
	res, err := d.Exec(`INSERT INTO groups (slug, name, admin_secret_hash, created_at) VALUES ('famille', 'Famille', zeroblob(32), ?)`, t0.Format(time.RFC3339Nano))
	if err != nil {
		t.Fatal(err)
	}
	gid, _ := res.LastInsertId()
	for _, n := range []string{"Alice", "Bob", "Chloe", "Dan"} {
		if _, err := d.Exec(`INSERT INTO participants (group_id, name, name_key, first_name, joined_at) VALUES (?, ?, ?, ?, ?)`,
			gid, "Zx"+n+"ovitch", n, n, t0.Format(time.RFC3339Nano)); err != nil {
			t.Fatal(err)
		}
	}
	e := &env{db: d, clock: clock.NewFake(t0), gid: gid,
		groups:  &fakeGroups{g: groups.Group{ID: gid, Slug: "famille"}},
		targets: &fakeTargets{by: map[int64][]int64{1: {2}}},
		letters: &fakeLetters{by: map[int64]*letter.Letter{}},
		people: &fakePeople{list: []participants.Participant{
			{ID: 1, Name: "Zxaliceovitch", FirstName: "Alice"}, {ID: 2, Name: "Zxbobovitch", FirstName: "Bob"},
			{ID: 3, Name: "Zxchloeovitch", FirstName: "Chloé"}, {ID: 4, Name: "Zxdanovitch", FirstName: "Dan"}}},
		search: &fakeSearch{}, images: &fakeImages{}}
	e.svc = NewService(d, e.clock, e.targets, e.groups, e.letters, e.people, e.search, e.images, opt)
	return e
}

func (e *env) req(giver int64) Request {
	return Request{Actor: participants.ParticipantActor(e.gid, giver), GroupID: e.gid}
}

func (e *env) setBudget(cents *int64, version int) {
	e.groups.g.BudgetCents, e.groups.g.BudgetVersion = cents, version
}

func cents(v int64) *int64 { return &v }

func sug(title, price string) sources.Suggestion {
	return sources.Suggestion{Title: title, URL: "https://boutique.example/" + title, Price: price, Source: "test"}
}

func wantKind(t *testing.T, err error, k errs.Kind) {
	t.Helper()
	if err == nil || !errs.Is(err, k) {
		t.Fatalf("erreur attendue de nature %s, obtenu %v", k, err)
	}
}

func titles(items []Item) []string {
	var out []string
	for _, i := range items {
		out = append(out, i.Title)
	}
	return out
}
