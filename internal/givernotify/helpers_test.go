package givernotify

import (
	"context"
	"database/sql"
	"fmt"
	"io"
	"log/slog"
	"math/rand/v2"
	"strings"
	"sync"
	"testing"
	"time"

	"secretsanta/internal/config"
	"secretsanta/internal/db"
	"secretsanta/internal/draw"
	"secretsanta/internal/groups"
	"secretsanta/internal/i18n/catalog"
	"secretsanta/internal/letter"
	"secretsanta/internal/letterread"
	"secretsanta/internal/mail/migrations"
	"secretsanta/internal/mail/queue"
	"secretsanta/internal/mailmsg"
	"secretsanta/internal/mailunsub"
	unsubmig "secretsanta/internal/mailunsub/migrations"
	"secretsanta/internal/participants"
	"secretsanta/internal/reminderrules"
	"secretsanta/internal/reminders"
	"secretsanta/internal/reveal"
	"secretsanta/internal/rules"
	"secretsanta/internal/shared/clock"
	"secretsanta/internal/shared/ports"
	"secretsanta/web"
)

const publicURL = "https://noel.example.org"

var t0 = time.Date(2026, 11, 15, 10, 0, 0, 0, time.UTC)

var ctx = context.Background()

var parisLoc = func() *time.Location {
	l, err := time.LoadLocation("Europe/Paris")
	if err != nil {
		panic(err)
	}
	return l
}()

type sqlDirectory struct{ db *sql.DB }

func (d sqlDirectory) DrawRole(ctx context.Context, groupID, id int64) (string, bool, error) {
	var role string
	err := d.db.QueryRowContext(ctx, `SELECT draw_role FROM participants WHERE group_id = ? AND id = ?`, groupID, id).Scan(&role)
	if err == sql.ErrNoRows {
		return "", false, nil
	}
	return role, err == nil, err
}

type groupLangStub struct{ lang string }

func (g groupLangStub) DefaultLanguage(context.Context, int64) (string, bool, error) {
	return g.lang, g.lang != "", nil
}

// recQueue garde chaque courriel mis en file et le passe à la vraie file.
type recQueue struct {
	svc   *queue.Service
	mu    sync.Mutex
	items []queue.Item
}

func (q *recQueue) EnqueueMail(ctx context.Context, it queue.Item) (int64, error) {
	q.mu.Lock()
	q.items = append(q.items, it)
	q.mu.Unlock()
	return q.svc.EnqueueMail(ctx, it)
}

// to renvoie les courriels mis en file pour une personne et un type.
func (q *recQueue) to(pid int64, typ string) []queue.Item {
	var out []queue.Item
	for _, it := range q.items {
		if it.ParticipantID == pid && (typ == "" || it.Type == typ) {
			out = append(out, it)
		}
	}
	return out
}

type transport struct {
	mu   sync.Mutex
	sent []queue.Envelope
}

func (t *transport) Send(_ context.Context, env queue.Envelope) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.sent = append(t.sent, env)
	return nil
}

func (t *transport) count() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	return len(t.sent)
}

type env struct {
	db      *sql.DB
	clock   *clock.Fake
	parts   *participants.Service
	draw    *draw.Service
	letters *letter.Service
	read    *letterread.Service
	unsub   *mailunsub.Service
	svc     *Service
	q       *recQueue
	tr      *transport
	mailq   *queue.Service
	revealA time.Time
}

func newEnv(t *testing.T, gl string) *env {
	t.Helper()
	d, err := db.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = d.Close() })
	steps := []struct {
		module string
		load   func() ([]db.Migration, error)
	}{
		{groups.Module, groups.Migrations},
		{participants.Module, participants.Migrations},
		{rules.Module, rules.Migrations},
		{letter.Module, letter.Migrations},
		{draw.Module, draw.Migrations},
		{reveal.Module, reveal.Migrations},
		{letterread.Module, letterread.Migrations},
		{reminderrules.Module, reminderrules.Migrations},
		{reminders.Module, reminders.Migrations},
		{unsubmig.Module, unsubmig.Migrations},
		{migrations.Module, migrations.Migrations},
	}
	for _, s := range steps {
		ms, err := s.load()
		if err != nil {
			t.Fatal(err)
		}
		if err := db.Migrate(d, s.module, ms); err != nil {
			t.Fatalf("%s : %v", s.module, err)
		}
	}
	c := clock.NewFake(t0)
	e := &env{db: d, clock: c, revealA: t0.Add(24 * time.Hour), tr: &transport{}}
	gs, err := groups.NewService(d, c, config.NewSecret("mot-de-passe-instance"))
	if err != nil {
		t.Fatal(err)
	}
	e.parts = participants.NewService(d, c, draw.NewState(d))
	r := rules.NewService(d, sqlDirectory{db: d})
	gate := reveal.NewGate(d, c)
	e.draw = draw.NewService(d, c, gs, e.parts, r, &ports.FakeRegistrationState{Pending: map[int64]int{}}, draw.Options{Gate: gate})
	bus := &ports.MemoryEventBus{}
	e.letters = letter.NewService(d, c, bus, t.TempDir())
	e.read = letterread.NewService(d, c, e.draw, e.letters)
	e.unsub, err = mailunsub.NewService(mailunsub.Options{DB: d, Clock: c, SigningKey: []byte("cle-de-signature-de-test-0123456789"), PublicURL: publicURL})
	if err != nil {
		t.Fatal(err)
	}
	b, err := mailmsg.New(mailmsg.Options{PublicURL: publicURL, From: "pere-noel@example.org", Clock: c, Random: rand.NewChaCha8([32]byte{1})})
	if err != nil {
		t.Fatal(err)
	}
	e.mailq, err = queue.New(queue.Options{DB: d, Clock: c, Builder: b, Transport: e.tr})
	if err != nil {
		t.Fatal(err)
	}
	e.q = &recQueue{svc: e.mailq}
	cat, err := catalog.Load(web.I18n, catalog.Options{DefaultLang: "fr", Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})
	if err != nil {
		t.Fatal(err)
	}
	e.svc = New(Options{
		DB: d, Clock: c, Rules: reminderrules.NewService(d, c, parisLoc),
		Directory: reminders.ServiceDirectory{DB: d, Groups: gs, People: e.parts, Langs: groupLangStub{lang: gl}},
		Queue:     e.q, Unsub: e.unsub, Suppress: &ports.FakeSuppressionList{}, Catalog: cat,
		Gate: gate, Letters: e.letters, PublicURL: publicURL,
	})
	e.svc.Subscribe(bus)
	return e
}

func (e *env) group(t *testing.T, slug string) int64 {
	t.Helper()
	res, err := e.db.Exec(`INSERT INTO groups (slug, name, admin_secret_hash, created_at, max_gifts_per_giver, reveal_at)
		VALUES (?, 'Famille', zeroblob(32), ?, 2, ?)`, slug, t0.Format(time.RFC3339Nano), e.revealA.Format(time.RFC3339))
	if err != nil {
		t.Fatal(err)
	}
	id, _ := res.LastInsertId()
	return id
}

func (e *env) add(t *testing.T, gid int64, first, locale string, role participants.DrawRole) int64 {
	t.Helper()
	p, err := e.parts.Create(ctx, participants.AdminActor(gid), gid, participants.Input{
		FirstName: first, Name: "Zq" + first + "enko", Role: role, Locale: locale,
		Email: strings.ToLower(first) + "@example.net",
	})
	if err != nil {
		t.Fatalf("ajout de %s : %v", first, err)
	}
	return p.ID
}

func (e *env) addN(t *testing.T, gid int64, prefix string, n int, role participants.DrawRole) []int64 {
	t.Helper()
	var ids []int64
	for i := 0; i < n; i++ {
		ids = append(ids, e.add(t, gid, fmt.Sprintf("%s%c", prefix, 'a'+i), "", role))
	}
	return ids
}

func (e *env) write(t *testing.T, gid int64, ids ...int64) {
	t.Helper()
	for _, id := range ids {
		if _, err := e.letters.AddIdea(ctx, gid, id, letter.IdeaInput{Title: fmt.Sprintf("Idée de %d", id)}); err != nil {
			t.Fatal(err)
		}
	}
}

// edit modifie la lettre de pid (publie l'événement de F5).
func (e *env) edit(t *testing.T, gid, pid int64, n int) {
	t.Helper()
	if _, err := e.letters.SetIntro(ctx, gid, pid, fmt.Sprintf("Version %d de la lettre", n)); err != nil {
		t.Fatal(err)
	}
}

func (e *env) mustDraw(t *testing.T, gid int64) {
	t.Helper()
	if _, err := e.draw.Draw(ctx, participants.AdminActor(gid), gid, draw.Request{Confirmed: true}); err != nil {
		t.Fatalf("tirage refusé : %v", err)
	}
}

type rawPair struct{ ID, Giver, Receiver int64 }

func (e *env) pairs(t *testing.T, gid int64) []rawPair {
	t.Helper()
	rows, err := e.db.Query(`SELECT p.id, p.giver_id, p.receiver_id FROM draw_pairs p JOIN draws d ON d.id = p.draw_id
		WHERE d.group_id = ? AND d.status = 'actuel' ORDER BY p.id`, gid)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []rawPair
	for rows.Next() {
		var p rawPair
		if err := rows.Scan(&p.ID, &p.Giver, &p.Receiver); err != nil {
			t.Fatal(err)
		}
		out = append(out, p)
	}
	return out
}

// giversPairs groupe les paires par tireur.
func (e *env) giversPairs(t *testing.T, gid int64) map[int64][]rawPair {
	m := map[int64][]rawPair{}
	for _, p := range e.pairs(t, gid) {
		m[p.Giver] = append(m[p.Giver], p)
	}
	return m
}

func (e *env) firstGiverWith(t *testing.T, gid int64, targets int) (int64, []rawPair) {
	t.Helper()
	for g, ps := range e.giversPairs(t, gid) {
		if len(ps) == targets {
			return g, ps
		}
	}
	t.Fatalf("aucun tireur à %d cible(s)", targets)
	return 0, nil
}

func (e *env) discover(t *testing.T, gid int64, p rawPair) {
	t.Helper()
	if _, err := e.draw.MarkDiscovered(ctx, participants.ParticipantActor(gid, p.Giver), gid, p.ID); err != nil {
		t.Fatalf("découverte : %v", err)
	}
}

// open fait lire la lettre de la paire à son tireur.
func (e *env) open(t *testing.T, gid int64, p rawPair) {
	t.Helper()
	e.discover(t, gid, p)
	if _, err := e.read.Read(ctx, participants.ParticipantActor(gid, p.Giver), gid, p.ID); err != nil {
		t.Fatalf("lecture : %v", err)
	}
}

// singleGroup : six personnes ordinaires (une cible chacune), lettres écrites,
// tirage fait, révélation ouverte.
func (e *env) singleGroup(t *testing.T, slug string) (int64, []int64) {
	t.Helper()
	gid := e.group(t, slug)
	all := e.addN(t, gid, "Mederic", 6, participants.RoleNormal)
	e.write(t, gid, all...)
	e.mustDraw(t, gid)
	e.clock.Set(e.revealA)
	return gid, all
}

// twoTargetsGroup : deux réceptions seules, deux « offre plus » (deux cibles),
// deux ordinaires ; lettres écrites, tirage fait, révélation ouverte.
func (e *env) twoTargetsGroup(t *testing.T, slug string) (int64, []int64) {
	t.Helper()
	gid := e.group(t, slug)
	var all []int64
	all = append(all, e.addN(t, gid, "Bertille", 2, participants.RoleReceptionSeule)...)
	all = append(all, e.addN(t, gid, "Evariste", 2, participants.RoleOffrePlus)...)
	all = append(all, e.addN(t, gid, "Mederic", 2, participants.RoleNormal)...)
	e.write(t, gid, all...)
	e.mustDraw(t, gid)
	e.clock.Set(e.revealA)
	return gid, all
}

func (e *env) sendMail(t *testing.T) int {
	t.Helper()
	n, err := e.mailq.Tick(ctx)
	if err != nil {
		t.Fatal(err)
	}
	return n
}

func (e *env) remind(t *testing.T) int {
	t.Helper()
	r, err := e.svc.Tick(ctx)
	if err != nil {
		t.Fatal(err)
	}
	return r.Queued
}

func (e *env) names(t *testing.T, gid int64) map[int64]string {
	t.Helper()
	rows, err := e.db.Query(`SELECT id, first_name FROM participants WHERE group_id = ?`, gid)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	m := map[int64]string{}
	for rows.Next() {
		var id int64
		var n string
		if err := rows.Scan(&id, &n); err != nil {
			t.Fatal(err)
		}
		m[id] = n
	}
	return m
}

// noOtherName vérifie que le message ne nomme personne d'autre que le
// destinataire (ni cible, ni autre participant).
func noOtherName(t *testing.T, e *env, gid, to int64, it queue.Item) {
	t.Helper()
	all := it.Subject + "\n" + it.Text + "\n" + it.HTML
	for id, n := range e.names(t, gid) {
		if id != to && strings.Contains(all, n) {
			t.Errorf("le message nomme %q : %s", n, all)
		}
	}
}
