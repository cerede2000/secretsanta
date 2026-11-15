package queue_test

import (
	"context"
	"database/sql"
	"math/rand/v2"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"secretsanta/internal/db"
	"secretsanta/internal/mail/migrations"
	"secretsanta/internal/mail/queue"
	"secretsanta/internal/mailmsg"
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
	ms, err := migrations.Migrations()
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Migrate(d, migrations.Module, ms); err != nil {
		t.Fatal(err)
	}
	return d
}

// fakeTransport mémorise les enveloppes et mesure la concurrence.
type fakeTransport struct {
	mu       sync.Mutex
	Sent     []queue.Envelope
	Times    []time.Time
	Clock    clock.Clock
	Fn       func(call int, env queue.Envelope) error
	inFlight atomic.Int32
	MaxSeen  atomic.Int32
	Hold     time.Duration
}

func (f *fakeTransport) Send(_ context.Context, env queue.Envelope) error {
	n := f.inFlight.Add(1)
	defer f.inFlight.Add(-1)
	for {
		m := f.MaxSeen.Load()
		if n <= m || f.MaxSeen.CompareAndSwap(m, n) {
			break
		}
	}
	if f.Hold > 0 {
		time.Sleep(f.Hold)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	call := len(f.Sent) + 1
	f.Sent = append(f.Sent, env)
	if f.Clock != nil {
		f.Times = append(f.Times, f.Clock.Now())
	}
	if f.Fn != nil {
		if err := f.Fn(call, env); err != nil {
			return err
		}
	}
	return nil
}

func (f *fakeTransport) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.Sent)
}

type env struct {
	DB    *sql.DB
	Clk   *clock.Fake
	Tr    *fakeTransport
	Supp  *ports.FakeSuppressionList
	Svc   *queue.Service
	Build *mailmsg.Builder
}

func builder(t *testing.T, clk clock.Clock) *mailmsg.Builder {
	t.Helper()
	b, err := mailmsg.New(mailmsg.Options{
		PublicURL: "https://noel.example.org", From: "pere-noel@example.org",
		Clock: clk, Random: rand.NewChaCha8([32]byte{1}),
	})
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func newEnv(t *testing.T, mod func(*queue.Options)) *env {
	t.Helper()
	return newEnvOn(t, openDB(t), clock.NewFake(t0), mod)
}

func newEnvOn(t *testing.T, d *sql.DB, clk *clock.Fake, mod func(*queue.Options)) *env {
	t.Helper()
	e := &env{DB: d, Clk: clk, Supp: &ports.FakeSuppressionList{}, Build: builder(t, clk)}
	e.Tr = &fakeTransport{Clock: clk}
	o := queue.Options{DB: d, Clock: clk, Builder: e.Build, Transport: e.Tr, Suppression: e.Supp}
	if mod != nil {
		mod(&o)
	}
	svc, err := queue.New(o)
	if err != nil {
		t.Fatal(err)
	}
	e.Svc = svc
	return e
}

// item fabrique un courriel conforme aux contrôles de mailmsg.
func item(to, name string, pid int64) queue.Item {
	text := "Bonjour " + name + ",\n\nVoici ton lien personnel : https://noel.example.org/famille/acces/p" + strconv.FormatInt(pid, 10) + "\n\nA bientôt."
	return queue.Item{
		GroupID: 1, ParticipantID: pid, To: to, ToName: name, Type: "invitation", Essential: true,
		Subject: "Ton invitation", Text: text, HTML: queue.HTMLFromText(text),
	}
}

func mustEnqueue(t *testing.T, e *env, it queue.Item) int64 {
	t.Helper()
	id, err := e.Svc.EnqueueMail(context.Background(), it)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func tick(t *testing.T, e *env) int {
	t.Helper()
	n, err := e.Svc.Tick(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return n
}

func status(t *testing.T, e *env, id int64) queue.Status {
	t.Helper()
	st, err := e.Svc.Status(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	return st
}

func contains(b []byte, s string) bool { return strings.Contains(string(b), s) }
