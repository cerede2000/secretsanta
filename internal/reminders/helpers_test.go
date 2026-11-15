package reminders

import (
	"context"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

	"secretsanta/internal/config"
	"secretsanta/internal/db"
	"secretsanta/internal/groups"
	"secretsanta/internal/i18n/catalog"
	"secretsanta/internal/mail/queue"
	"secretsanta/internal/mailunsub"
	unsubmig "secretsanta/internal/mailunsub/migrations"
	"secretsanta/internal/participants"
	"secretsanta/internal/reminderrules"
	"secretsanta/internal/shared/clock"
	"secretsanta/internal/shared/ports"
	"secretsanta/web"
)

const publicURL = "https://noel.example.org"

var parisLoc = func() *time.Location {
	l, err := time.LoadLocation("Europe/Paris")
	if err != nil {
		panic(err)
	}
	return l
}()

// paris construit un instant en heure de Paris.
func paris(y int, m time.Month, d, h, mi int) time.Time {
	return time.Date(y, m, d, h, mi, 0, 0, parisLoc)
}

type fakeDir struct {
	mu     sync.Mutex
	groups map[int64]GroupInfo
	people map[int64][]participants.Participant
	nextID int64
}

func (d *fakeDir) GroupIDs(context.Context) ([]int64, error) {
	var out []int64
	for id := range d.groups {
		out = append(out, id)
	}
	return out, nil
}
func (d *fakeDir) Group(_ context.Context, id int64) (GroupInfo, error) { return d.groups[id], nil }
func (d *fakeDir) Participants(_ context.Context, id int64) ([]participants.Participant, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	return append([]participants.Participant(nil), d.people[id]...), nil
}

type fakeFacts struct {
	linkOpened map[int64]bool
	written    map[int64]bool
	unread     map[int64]bool
}

func (f *fakeFacts) LinkOpened(_ context.Context, _, pid int64) (bool, error) {
	return f.linkOpened[pid], nil
}
func (f *fakeFacts) LetterWritten(_ context.Context, _, pid int64) (bool, error) {
	return f.written[pid], nil
}
func (f *fakeFacts) UnreadGivers(context.Context, int64) (map[int64]bool, error) {
	return f.unread, nil
}

type fakeQueue struct{ items []queue.Item }

func (q *fakeQueue) EnqueueMail(_ context.Context, it queue.Item) (int64, error) {
	q.items = append(q.items, it)
	return int64(len(q.items)), nil
}

type env struct {
	eng   *Engine
	clk   *clock.Fake
	dir   *fakeDir
	facts *fakeFacts
	q     *fakeQueue
	sup   *ports.FakeSuppressionList
	unsub *mailunsub.Service
	rules *reminderrules.Service
	gid   int64
}

// newEnv démarre à start ; le groupe est en français sauf indication.
func newEnv(t *testing.T, start time.Time) *env {
	t.Helper()
	d, err := db.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = d.Close() })
	gm, err := groups.Migrations()
	if err != nil {
		t.Fatal(err)
	}
	must(t, db.Migrate(d, groups.Module, gm))
	rm, err := reminderrules.Migrations()
	if err != nil {
		t.Fatal(err)
	}
	must(t, db.Migrate(d, reminderrules.Module, rm))
	mm, err := Migrations()
	if err != nil {
		t.Fatal(err)
	}
	must(t, db.Migrate(d, Module, mm))
	um, err := unsubmig.Migrations()
	if err != nil {
		t.Fatal(err)
	}
	must(t, db.Migrate(d, unsubmig.Module, um))

	clk := clock.NewFake(start)
	gs, err := groups.NewService(d, clk, config.NewSecret("mdp-instance"))
	if err != nil {
		t.Fatal(err)
	}
	r, err := gs.Create(context.Background(), groups.CreateInput{InstancePassword: "mdp-instance", ClientKey: "c", Name: "Famille Martin", Slug: "famille"})
	if err != nil {
		t.Fatal(err)
	}
	gid := r.Group.ID
	cat, err := catalog.Load(web.I18n, catalog.Options{DefaultLang: "fr", Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})
	if err != nil {
		t.Fatal(err)
	}
	unsub, err := mailunsub.NewService(mailunsub.Options{DB: d, Clock: clk, SigningKey: []byte("cle-de-signature-de-test-0123456789"), PublicURL: publicURL})
	if err != nil {
		t.Fatal(err)
	}
	rules := reminderrules.NewService(d, clk, parisLoc)
	e := &env{
		clk: clk, q: &fakeQueue{}, sup: &ports.FakeSuppressionList{}, unsub: unsub, rules: rules, gid: gid,
		dir:   &fakeDir{groups: map[int64]GroupInfo{gid: {ID: gid, Slug: "famille", Name: "Famille Martin", Lang: "fr"}}, people: map[int64][]participants.Participant{}},
		facts: &fakeFacts{linkOpened: map[int64]bool{}, written: map[int64]bool{}, unread: map[int64]bool{}},
	}
	e.eng = New(Options{DB: d, Clock: clk, Rules: rules, Directory: e.dir, Facts: e.facts, Queue: e.q,
		Unsub: unsub, Suppress: e.sup, Catalog: cat, PublicURL: publicURL})
	return e
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

// add ajoute un participant du groupe et renvoie son identifiant.
func (e *env) add(p participants.Participant) int64 {
	e.dir.nextID++
	p.ID = e.dir.nextID
	p.GroupID = e.gid
	if p.Role == "" {
		p.Role = participants.RoleNormal
	}
	if p.FirstName == "" {
		p.FirstName = p.Name
	}
	e.dir.people[e.gid] = append(e.dir.people[e.gid], p)
	return p.ID
}

func (e *env) tick(t *testing.T) int {
	t.Helper()
	r, err := e.eng.Tick(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return r.Queued
}

// to renvoie les courriels en file pour une personne.
func (e *env) to(pid int64) []queue.Item {
	var out []queue.Item
	for _, it := range e.q.items {
		if it.ParticipantID == pid {
			out = append(out, it)
		}
	}
	return out
}
