package verify

import (
	"context"
	"database/sql"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"secretsanta/internal/access/migrations"
	"secretsanta/internal/access/sqlstore"
	"secretsanta/internal/access/token"
	"secretsanta/internal/config"
	"secretsanta/internal/db"
	"secretsanta/internal/enrolment"
	"secretsanta/internal/groups"
	"secretsanta/internal/participants"
	"secretsanta/internal/registration/code"
	"secretsanta/internal/registration/invite"
	"secretsanta/internal/shared/clock"
	"secretsanta/internal/shared/ports"
	"secretsanta/internal/shared/scheduler"
)

var t0 = time.Date(2026, 11, 15, 10, 0, 0, 0, time.UTC)

var (
	signingKey = []byte("cle-de-signature-de-test-0123456789")
)

// codeMails garde les courriels « code de vérification ».
type codeMails struct {
	mu   sync.Mutex
	sent []code.Mail
}

func (c *codeMails) SendVerification(_ context.Context, m code.Mail) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.sent = append(c.sent, m)
	return nil
}

// last renvoie le dernier courriel de l'inscription.
func (c *codeMails) last(t *testing.T, groupID, regID int64) code.Mail {
	t.Helper()
	c.mu.Lock()
	defer c.mu.Unlock()
	for i := len(c.sent) - 1; i >= 0; i-- {
		if c.sent[i].GroupID == groupID && c.sent[i].RegistrationID == regID {
			return c.sent[i]
		}
	}
	t.Fatalf("aucun code pour l'inscription %d du groupe %d", regID, groupID)
	return code.Mail{}
}

func (c *codeMails) count() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.sent)
}

// welcomes garde les courriels « bienvenue ».
type welcomes struct {
	mu   sync.Mutex
	sent []Welcome
}

func (w *welcomes) SendWelcome(_ context.Context, m Welcome) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.sent = append(w.sent, m)
	return nil
}

func (w *welcomes) all() []Welcome {
	w.mu.Lock()
	defer w.mu.Unlock()
	return append([]Welcome(nil), w.sent...)
}

type env struct {
	db       *sql.DB
	clock    *clock.Fake
	draw     *ports.FakeDrawState
	groups   *groups.Service
	parts    *participants.Service
	tokens   *token.Service
	pending  *invite.MemoryStore
	codes    *code.Service
	codeMail *codeMails
	welcome  *welcomes
	sched    *scheduler.Scheduler
	store    *MemoryStore
	inv      *invite.Service
	svc      *Service
	g1, g2   int64
}

func insertGroup(t *testing.T, d *sql.DB, slug string) int64 {
	t.Helper()
	res, err := d.Exec(`INSERT INTO groups (slug, name, admin_secret_hash, created_at) VALUES (?, ?, zeroblob(32), ?)`,
		slug, "Groupe "+slug, t0.Format(time.RFC3339))
	if err != nil {
		t.Fatal(err)
	}
	id, _ := res.LastInsertId()
	return id
}

func migrate(t *testing.T, d *sql.DB, module string, ms []db.Migration, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Migrate(d, module, ms); err != nil {
		t.Fatal(err)
	}
}

func newEnv(t *testing.T) *env {
	t.Helper()
	d, err := db.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = d.Close() })
	gm, err := groups.Migrations()
	migrate(t, d, groups.Module, gm, err)
	pm, err := participants.Migrations()
	migrate(t, d, participants.Module, pm, err)
	am, err := migrations.Migrations()
	migrate(t, d, migrations.Module, am, err)

	e := &env{db: d, clock: clock.NewFake(t0), draw: &ports.FakeDrawState{Done: map[int64]bool{}},
		pending: invite.NewMemoryStore(), codeMail: &codeMails{}, welcome: &welcomes{}, store: NewMemoryStore()}
	e.sched = scheduler.New(e.clock)
	if e.groups, err = groups.NewService(d, e.clock, config.Secret{}); err != nil {
		t.Fatal(err)
	}
	e.parts = participants.NewService(d, e.clock, e.draw)
	if e.tokens, err = token.NewService(sqlstore.New(d), token.LoginCodeLength, signingKey); err != nil {
		t.Fatal(err)
	}
	enrol := enrolment.NewService(d, e.parts, e.tokens, func(slug, tok string) string {
		return "https://noel.example.org/" + slug + "/acces/" + tok
	})
	if e.codes, err = code.New(code.Options{Store: code.NewMemoryStore(), Clock: e.clock, Mailer: e.codeMail,
		Link: func(slug, tok string) string {
			return "https://noel.example.org/" + slug + "/inscription/confirmer/" + tok
		}, Key: signingKey}); err != nil {
		t.Fatal(err)
	}
	if e.svc, err = New(Config{Pending: e.pending, Store: e.store, Codes: e.codes, Enrol: enrol,
		Groups: e.groups, Draw: e.draw, Clock: e.clock, Welcome: e.welcome, Scheduler: e.sched}); err != nil {
		t.Fatal(err)
	}
	if e.inv, err = invite.New(invite.Config{Groups: e.groups, Directory: invite.SQLDirectory{DB: d},
		Store: e.pending, Draw: e.draw, Clock: e.clock, Key: signingKey, Codes: e.svc}); err != nil {
		t.Fatal(err)
	}
	e.g1 = insertGroup(t, d, "noel")
	e.g2 = insertGroup(t, d, "famille")
	return e
}

func admin(g int64) participants.Actor { return participants.AdminActor(g) }

// registration est une inscription en attente créée par le formulaire.
type registration struct {
	group int64
	slug  string
	id    int64
	ref   string
	email string
}

// register active le mode si besoin, soumet le formulaire et renvoie
// l'inscription en attente créée.
func (e *env) register(t *testing.T, g int64, first, name, email string) registration {
	t.Helper()
	ctx := t.Context()
	grp, err := e.groups.ByID(ctx, g)
	if err != nil {
		t.Fatal(err)
	}
	l, err := e.inv.Enable(ctx, admin(g), g)
	if err != nil {
		t.Fatal(err)
	}
	res, err := e.inv.Submit(ctx, grp.Slug, l.Token, invite.Submission{FirstName: first, Name: name, Email: email, IP: "203.0.113.7"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Outcome != invite.OutcomeCodeSent || res.Ref == "" {
		t.Fatalf("inscription refusée : %+v", res)
	}
	ps, err := e.pending.ListPending(ctx, g, e.clock.Now())
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range ps {
		if p.Ref == res.Ref {
			return registration{group: g, slug: grp.Slug, id: p.ID, ref: p.Ref, email: email}
		}
	}
	t.Fatal("inscription en attente introuvable")
	return registration{}
}

// code renvoie le dernier code envoyé à l'inscription.
func (e *env) code(t *testing.T, r registration) string {
	t.Helper()
	return e.codeMail.last(t, r.group, r.id).Code
}

// confirmToken renvoie le jeton du dernier lien de confirmation.
func (e *env) confirmToken(t *testing.T, r registration) string {
	t.Helper()
	link := e.codeMail.last(t, r.group, r.id).Link
	return link[strings.LastIndex(link, "/")+1:]
}

func (e *env) participants(t *testing.T, g int64) []participants.Participant {
	t.Helper()
	ps, err := e.parts.List(t.Context(), admin(g), g)
	if err != nil {
		t.Fatal(err)
	}
	return ps
}

func (e *env) pendingCount(t *testing.T, g int64) int {
	t.Helper()
	n, err := e.svc.PendingCount(t.Context(), g)
	if err != nil {
		t.Fatal(err)
	}
	return n
}

func (e *env) credentialRows(t *testing.T) int {
	t.Helper()
	var n int
	if err := e.db.QueryRow(`SELECT COUNT(*) FROM access_credentials`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// mustRefused vérifie le refus uniforme du code.
func mustRefused(t *testing.T, err error) {
	t.Helper()
	if !code.IsRefused(err) {
		t.Fatalf("refus uniforme attendu, obtenu %v", err)
	}
}

// noPairFields échoue si le type (et ses champs, récursivement) porte un
// champ de paire, de cible, de tireur ou de destinataire du tirage.
func noPairFields(t *testing.T, v any) {
	t.Helper()
	seen := map[reflect.Type]bool{}
	var walk func(reflect.Type)
	walk = func(rt reflect.Type) {
		for rt.Kind() == reflect.Pointer || rt.Kind() == reflect.Slice {
			rt = rt.Elem()
		}
		if rt.Kind() != reflect.Struct || seen[rt] {
			return
		}
		seen[rt] = true
		for i := 0; i < rt.NumField(); i++ {
			f := rt.Field(i)
			n := strings.ToLower(f.Name)
			for _, bad := range []string{"pair", "target", "cible", "giver", "receiver", "recipient", "tireur"} {
				if strings.Contains(n, bad) {
					t.Errorf("%s.%s ressemble à une paire", rt.Name(), f.Name)
				}
			}
			walk(f.Type)
		}
	}
	walk(reflect.TypeOf(v))
}
