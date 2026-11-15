package code

import (
	"context"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"secretsanta/internal/shared/clock"
)

var (
	testStart = time.Date(2026, 12, 1, 9, 0, 0, 0, time.UTC)
	testKey   = []byte("clé-de-signature-de-test")
)

const testGroup int64 = 7

// captureMailer garde les courriels envoyés.
type captureMailer struct {
	mu   sync.Mutex
	sent []Mail
}

func (c *captureMailer) SendVerification(_ context.Context, m Mail) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.sent = append(c.sent, m)
	return nil
}

func (c *captureMailer) last(t *testing.T) Mail {
	t.Helper()
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.sent) == 0 {
		t.Fatal("aucun courriel envoyé")
	}
	return c.sent[len(c.sent)-1]
}

func (c *captureMailer) count() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.sent)
}

type fixture struct {
	svc    *Service
	store  *MemoryStore
	clk    *clock.Fake
	mailer *captureMailer
}

func testLink(slug, tok string) string {
	return "https://noel.example.org/" + slug + "/inscription/confirmer/" + tok
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	f := &fixture{store: NewMemoryStore(), clk: clock.NewFake(testStart), mailer: &captureMailer{}}
	svc, err := New(Options{Store: f.store, Clock: f.clk, Mailer: f.mailer, Link: testLink, Key: testKey})
	if err != nil {
		t.Fatal(err)
	}
	f.svc = svc
	return f
}

func request(group, reg int64, email string) Request {
	return Request{
		GroupID: group, RegistrationID: reg, Slug: "famille" + strconv.FormatInt(group, 10),
		GroupName: "Famille", To: email, FirstName: "Léa",
	}
}

// issue émet un code et renvoie le courriel envoyé.
func (f *fixture) issue(t *testing.T, group, reg int64, email string) Mail {
	t.Helper()
	if _, err := f.svc.Issue(context.Background(), request(group, reg, email)); err != nil {
		t.Fatalf("émission (groupe %d, inscription %d) : %v", group, reg, err)
	}
	return f.mailer.last(t)
}

// tokenOf extrait le jeton du lien de confirmation.
func tokenOf(t *testing.T, m Mail) string {
	t.Helper()
	i := strings.LastIndex(m.Link, "/")
	if i < 0 {
		t.Fatalf("lien sans jeton : %q", m.Link)
	}
	return m.Link[i+1:]
}

// wrongCode renvoie un code bien formé différent de c.
func wrongCode(c string) string {
	if c[0] == '2' {
		return "3" + c[1:]
	}
	return "2" + c[1:]
}
