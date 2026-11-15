package sqlstore

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"sync"
	"testing"
	"time"

	"secretsanta/internal/config"
	"secretsanta/internal/db"
	"secretsanta/internal/groups"
	"secretsanta/internal/participants"
	"secretsanta/internal/registration/code"
	"secretsanta/internal/registration/invite"
	"secretsanta/internal/registration/migrations"
	"secretsanta/internal/shared/clock"
	"secretsanta/internal/shared/ports"
)

var t0 = time.Date(2026, 11, 15, 10, 0, 0, 0, time.UTC)

// testKey tient lieu de SIGNING_SECRET.
var testKey = []byte("cle-de-signature-registration-0123456789")

// openMigrated ouvre une base vide dans dir et applique, dans l'ordre du
// registre, les migrations des groupes, des participants et de l'inscription.
func openMigrated(t testing.TB, dir string) *sql.DB {
	t.Helper()
	d, err := db.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = d.Close() })
	migrateAll(t, d)
	return d
}

func migrateAll(t testing.TB, d *sql.DB) {
	t.Helper()
	gm, err := groups.Migrations()
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Migrate(d, groups.Module, gm); err != nil {
		t.Fatal(err)
	}
	pm, err := participants.Migrations()
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Migrate(d, participants.Module, pm); err != nil {
		t.Fatal(err)
	}
	rm, err := migrations.Migrations()
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Migrate(d, migrations.Module, rm); err != nil {
		t.Fatal(err)
	}
}

func insertGroup(t testing.TB, d *sql.DB, slug string) int64 {
	t.Helper()
	res, err := d.Exec(`INSERT INTO groups (slug, name, admin_secret_hash, created_at) VALUES (?, ?, zeroblob(32), ?)`,
		slug, "Groupe "+slug, t0.Format(time.RFC3339))
	if err != nil {
		t.Fatal(err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		t.Fatal(err)
	}
	return id
}

// hexHash fabrique une empreinte hexadécimale de 64 caractères.
func hexHash(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:])
}

// rawHash fabrique un condensé brut de 32 octets.
func rawHash(s string) []byte {
	h := sha256.Sum256([]byte("invite:" + s))
	return h[:]
}

func countRows(t testing.TB, d *sql.DB, table string) int {
	t.Helper()
	var n int
	if err := d.QueryRow(`SELECT COUNT(*) FROM ` + table).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// mailbox capture les courriels de vérification (code et lien en clair).
type mailbox struct {
	mu   sync.Mutex
	sent []code.Mail
}

func (m *mailbox) SendVerification(_ context.Context, mail code.Mail) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sent = append(m.sent, mail)
	return nil
}

func (m *mailbox) last(t testing.TB) code.Mail {
	t.Helper()
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.sent) == 0 {
		t.Fatal("aucun courriel de vérification")
	}
	return m.sent[len(m.sent)-1]
}

// codeIssuer relie le formulaire (invite) au service des codes, comme le
// fera l'orchestrateur.
type codeIssuer struct {
	svc  *code.Service
	slug map[int64]string
}

func (c codeIssuer) IssueCode(ctx context.Context, p invite.Pending) error {
	_, err := c.svc.Issue(ctx, code.Request{
		GroupID: p.GroupID, RegistrationID: p.ID, Slug: c.slug[p.GroupID], GroupName: "Groupe",
		To: p.Email, FirstName: p.FirstName, Lang: p.Lang,
	})
	return err
}

// env assemble les services réels de l'inscription sur les magasins SQL.
type env struct {
	dir     string
	db      *sql.DB
	clock   *clock.Fake
	invites *InviteStore
	codes   *CodeStore
	mail    *mailbox
	codeSvc *code.Service
	svc     *invite.Service
	parts   *participants.Service
	g1, g2  int64
}

func newEnv(t *testing.T) *env {
	t.Helper()
	dir := t.TempDir()
	d := openMigrated(t, dir)
	c := clock.NewFake(t0)
	e := &env{dir: dir, db: d, clock: c, invites: NewInviteStore(d), codes: NewCodeStore(d), mail: &mailbox{}}
	e.g1 = insertGroup(t, d, "noel")
	e.g2 = insertGroup(t, d, "famille")
	var err error
	e.codeSvc, err = code.New(code.Options{
		Store: e.codes, Clock: c, Mailer: e.mail, Key: testKey,
		Link: func(slug, tok string) string { return "https://exemple.test/" + slug + "/inscription/confirmer/" + tok },
	})
	if err != nil {
		t.Fatal(err)
	}
	gs, err := groups.NewService(d, c, config.Secret{})
	if err != nil {
		t.Fatal(err)
	}
	draw := &ports.FakeDrawState{Done: map[int64]bool{}}
	e.parts = participants.NewService(d, c, draw)
	e.svc, err = invite.New(invite.Config{
		Groups: gs, Directory: invite.SQLDirectory{DB: d}, Store: e.invites, Draw: draw, Clock: c,
		Key:   testKey,
		Codes: codeIssuer{svc: e.codeSvc, slug: map[int64]string{e.g1: "noel", e.g2: "famille"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	return e
}

func (e *env) enable(t *testing.T, g int64) *invite.Link {
	t.Helper()
	l, err := e.svc.Enable(t.Context(), participants.AdminActor(g), g)
	if err != nil {
		t.Fatal(err)
	}
	return l
}

func (e *env) submit(t *testing.T, slug, tok string, s invite.Submission) *invite.Result {
	t.Helper()
	r, err := e.svc.Submit(t.Context(), slug, tok, s)
	if err != nil {
		t.Fatalf("soumission : %v", err)
	}
	return r
}

func submission(first, name, email string) invite.Submission {
	return invite.Submission{FirstName: first, Name: name, Email: email, IP: "203.0.113.7"}
}

// pendingByRef retrouve une inscription en attente par sa référence.
func (e *env) pendingByRef(t *testing.T, g int64, ref string) invite.Pending {
	t.Helper()
	ps, err := e.invites.ListPending(t.Context(), g, e.clock.Now())
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range ps {
		if p.Ref == ref {
			return p
		}
	}
	t.Fatalf("inscription %q introuvable", ref)
	return invite.Pending{}
}

func adminOf(g int64) participants.Actor { return participants.AdminActor(g) }

// codeRequest décrit l'inscription p pour un (nouvel) envoi de code.
func codeRequest(slug string, p invite.Pending) code.Request {
	return code.Request{GroupID: p.GroupID, RegistrationID: p.ID, Slug: slug, GroupName: "Groupe",
		To: p.Email, FirstName: p.FirstName, Lang: p.Lang}
}
