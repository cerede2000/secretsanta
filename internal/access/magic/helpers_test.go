package magic_test

import (
	"context"
	"database/sql"
	"strings"
	"sync"
	"testing"
	"time"

	"secretsanta/internal/access/limiter"
	"secretsanta/internal/access/magic"
	"secretsanta/internal/db"
	"secretsanta/internal/shared/clock"
	"secretsanta/internal/shared/ports"
)

var t0 = time.Date(2026, 12, 1, 10, 0, 0, 0, time.UTC)

func openDB(t *testing.T) *sql.DB {
	t.Helper()
	d, err := db.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = d.Close() })
	ms, err := magic.Migrations()
	if err != nil {
		t.Fatal(err)
	}
	if len(ms) == 0 {
		t.Fatal("aucune migration embarquée")
	}
	if err := db.Migrate(d, magic.Module, ms); err != nil {
		t.Fatal(err)
	}
	return d
}

// directory est un annuaire de participants : groupe -> adresse -> id.
type directory struct {
	mu    sync.Mutex
	known map[int64]map[string]int64
	delay time.Duration
	calls int
}

func (d *directory) FindByEmail(_ context.Context, groupID int64, email string) (int64, bool, error) {
	d.mu.Lock()
	d.calls++
	id, ok := d.known[groupID][strings.ToLower(email)]
	d.mu.Unlock()
	if ok && d.delay > 0 {
		time.Sleep(d.delay)
	}
	return id, ok, nil
}

// sender mémorise les courriels, sûr en concurrence.
type sender struct {
	mu    sync.Mutex
	sent  []ports.EmailMessage
	delay time.Duration
}

func (s *sender) Send(_ context.Context, m ports.EmailMessage) error {
	if s.delay > 0 {
		time.Sleep(s.delay)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sent = append(s.sent, m)
	return nil
}

func (s *sender) messages() []ports.EmailMessage {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]ports.EmailMessage(nil), s.sent...)
}

const linkPrefix = "https://noel.example/"

func link(slug, token string) string { return linkPrefix + slug + "/lien/" + token }

func compose(to, l string) ports.EmailMessage {
	return ports.EmailMessage{To: to, Subject: "Ton lien de connexion", Body: "Lien valable 15 minutes : " + l}
}

func tokenFrom(t *testing.T, m ports.EmailMessage) string {
	t.Helper()
	i := strings.Index(m.Body, "/lien/")
	if i < 0 {
		t.Fatalf("aucun lien dans le courriel : %q", m.Body)
	}
	return m.Body[i+len("/lien/"):]
}

type fixture struct {
	db  *sql.DB
	clk *clock.Fake
	dir *directory
	out *sender
	svc *magic.Service
}

func newFixture(t *testing.T, rule *limiter.Rule) *fixture {
	t.Helper()
	f := &fixture{
		db:  openDB(t),
		clk: clock.NewFake(t0),
		dir: &directory{known: map[int64]map[string]int64{
			1: {"anne@example.org": 11, "bob@example.org": 12},
			2: {"anne@example.org": 21},
		}},
		out: &sender{},
	}
	var lim *limiter.Limiter
	if rule != nil {
		lim = limiter.New(f.clk, *rule)
	}
	svc, err := magic.New(magic.Config{
		DB: f.db, Clock: f.clk, Directory: f.dir, Sender: f.out,
		Link: link, Compose: compose, Limiter: lim,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(svc.Wait)
	f.svc = svc
	return f
}
