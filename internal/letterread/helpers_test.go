package letterread

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"testing"
	"time"

	"secretsanta/internal/config"
	"secretsanta/internal/db"
	"secretsanta/internal/draw"
	"secretsanta/internal/groups"
	"secretsanta/internal/letter"
	"secretsanta/internal/participants"
	"secretsanta/internal/reveal"
	"secretsanta/internal/rules"
	"secretsanta/internal/shared/clock"
	"secretsanta/internal/shared/errs"
	"secretsanta/internal/shared/ports"
)

var t0 = time.Date(2026, 11, 15, 10, 0, 0, 0, time.UTC)

var ctx = context.Background()

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
	db      *sql.DB
	clock   *clock.Fake
	groups  *groups.Service
	parts   *participants.Service
	draw    *draw.Service
	letters *letter.Service
	svc     *Service
	revealA time.Time
}

func newEnv(t *testing.T) *env {
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
	c := clock.NewFake(t0)
	g, err := groups.NewService(d, c, config.NewSecret("mot-de-passe-instance"))
	if err != nil {
		t.Fatal(err)
	}
	e := &env{db: d, clock: c, groups: g, revealA: t0.Add(24 * time.Hour)}
	e.parts = participants.NewService(d, c, draw.NewState(d))
	r := rules.NewService(d, sqlDirectory{db: d})
	e.draw = draw.NewService(d, c, g, e.parts, r, &ports.FakeRegistrationState{Pending: map[int64]int{}},
		draw.Options{Gate: reveal.NewGate(d, c)})
	e.letters = letter.NewService(d, c, &ports.MemoryEventBus{}, t.TempDir())
	e.svc = NewService(d, c, e.draw, e.letters)
	return e
}

// group crée un groupe avec une date de révélation à t0 + 24 h.
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

func (e *env) add(t *testing.T, gid int64, first string, role participants.DrawRole) int64 {
	t.Helper()
	p, err := e.parts.Create(ctx, participants.AdminActor(gid), gid,
		participants.Input{FirstName: first, Name: "Zq" + first + "enko", Role: role})
	if err != nil {
		t.Fatalf("ajout de %s : %v", first, err)
	}
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

// ideaTitle est le titre de la première idée écrite par pid.
func ideaTitle(pid int64) string { return fmt.Sprintf("Idée de %d", pid) }

// write fait écrire à chacun une lettre d'une idée identifiable.
func (e *env) write(t *testing.T, gid int64, ids ...int64) {
	t.Helper()
	for _, id := range ids {
		if _, err := e.letters.AddIdea(ctx, gid, id, letter.IdeaInput{Title: ideaTitle(id)}); err != nil {
			t.Fatal(err)
		}
	}
}

func (e *env) mustDraw(t *testing.T, gid int64) {
	t.Helper()
	if _, err := e.draw.Draw(ctx, admin(gid), gid, draw.Request{Confirmed: true}); err != nil {
		t.Fatalf("tirage refusé : %v", err)
	}
}

func (e *env) openReveal() { e.clock.Set(e.revealA) }

type rawPair struct {
	ID       int64
	Giver    int64
	Receiver int64
}

// pairs lit les paires du tirage actuel (le test seul y a accès).
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

func (e *env) pairOf(t *testing.T, gid, receiver int64) rawPair {
	t.Helper()
	for _, p := range e.pairs(t, gid) {
		if p.Receiver == receiver {
			return p
		}
	}
	t.Fatalf("aucune paire pour %d", receiver)
	return rawPair{}
}

func (e *env) discover(t *testing.T, gid, giver, pairID int64) {
	t.Helper()
	if _, err := e.draw.MarkDiscovered(ctx, who(gid, giver), gid, pairID); err != nil {
		t.Fatalf("découverte : %v", err)
	}
}

func (e *env) discoverAll(t *testing.T, gid int64) {
	t.Helper()
	for _, p := range e.pairs(t, gid) {
		e.discover(t, gid, p.Giver, p.ID)
	}
}

func (e *env) mustRead(t *testing.T, gid, giver, pairID, receiver int64) *letter.Letter {
	t.Helper()
	l, err := e.svc.Read(ctx, who(gid, giver), gid, pairID)
	if err != nil {
		t.Fatalf("lecture refusée au tireur : %v", err)
	}
	if len(l.Ideas) == 0 || l.Ideas[0].Title != ideaTitle(receiver) {
		t.Fatalf("lettre inattendue : %+v", l)
	}
	return l
}

func wantRefused(t *testing.T, err error) {
	t.Helper()
	if err == nil {
		t.Fatal("lecture acceptée, refus attendu")
	}
	if !errs.Is(err, errs.KindNotFound) && !errs.Is(err, errs.KindForbidden) && !errs.Is(err, errs.KindUnauthorized) {
		t.Fatalf("refus attendu, obtenu %v", err)
	}
}

func (e *env) stamp(t *testing.T, gid, pid int64) bool {
	t.Helper()
	s, err := e.svc.Stamp(ctx, who(gid, pid), gid)
	if err != nil {
		t.Fatal(err)
	}
	return s.Read
}

// adjust remplace les paires et archive les lettres des tirés dont le tireur
// change, dans une seule transaction, comme le fera l'ajustement (F4c).
func (e *env) adjust(t *testing.T, gid int64, after []draw.Pair) []int64 {
	t.Helper()
	var before []draw.Pair
	for _, p := range e.pairs(t, gid) {
		before = append(before, draw.Pair{Giver: p.Giver, Receiver: p.Receiver})
	}
	changed := ChangedReceivers(before, after)
	tx, err := e.db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback() }()
	var arch Archiver = e.svc
	if err := arch.ArchiveTx(ctx, tx, gid, changed); err != nil {
		t.Fatal(err)
	}
	if err := e.draw.AdjustTx(ctx, tx, gid, after); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	return changed
}

// swap échange les tirés de deux paires (paire i : g1→r1 ; on cherche une
// paire g2→r2 avec g2 ≠ g1, g2 ≠ r1 et g1 ≠ r2, r2 hors des cibles de g1) ;
// renvoie les nouvelles paires et l'autre tireur et son ancien tiré.
func (e *env) swap(t *testing.T, gid int64, receiver int64) (after []draw.Pair, otherGiver, otherReceiver int64) {
	t.Helper()
	ps := e.pairs(t, gid)
	var me rawPair
	for _, p := range ps {
		if p.Receiver == receiver {
			me = p
		}
	}
	mine := map[int64]bool{}
	for _, p := range ps {
		if p.Giver == me.Giver {
			mine[p.Receiver] = true
		}
	}
	for _, o := range ps {
		if o.Giver == me.Giver || o.Giver == me.Receiver || o.Receiver == me.Giver || mine[o.Receiver] {
			continue
		}
		for _, p := range ps {
			switch p.ID {
			case me.ID:
				after = append(after, draw.Pair{Giver: me.Giver, Receiver: o.Receiver})
			case o.ID:
				after = append(after, draw.Pair{Giver: o.Giver, Receiver: me.Receiver})
			default:
				after = append(after, draw.Pair{Giver: p.Giver, Receiver: p.Receiver})
			}
		}
		return after, o.Giver, o.Receiver
	}
	t.Fatal("aucun échange possible")
	return nil, 0, 0
}

// archives compte les archives de la lettre de pid.
func (e *env) archives(t *testing.T, pid int64) int {
	t.Helper()
	var n int
	if err := e.db.QueryRow(`SELECT count(*) FROM letter_archives a JOIN letters l ON l.id = a.letter_id
		WHERE l.participant_id = ?`, pid).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func pngBytes(t *testing.T) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 4, 4))
	img.Set(0, 0, color.RGBA{255, 0, 0, 255})
	var b bytes.Buffer
	if err := png.Encode(&b, img); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

// giverOfTwo renvoie un tireur qui a deux cibles et ses deux paires.
func (e *env) giverOfTwo(t *testing.T, gid int64) (int64, []rawPair) {
	t.Helper()
	by := map[int64][]rawPair{}
	var order []int64
	for _, p := range e.pairs(t, gid) {
		if len(by[p.Giver]) == 0 {
			order = append(order, p.Giver)
		}
		by[p.Giver] = append(by[p.Giver], p)
	}
	for _, g := range order {
		if len(by[g]) == 2 {
			return g, by[g]
		}
	}
	t.Fatal("aucun tireur à deux cibles")
	return 0, nil
}

// twoTargetsGroup crée un groupe où des tireurs « J'offre plus » ont deux
// cibles ; tout le monde a écrit sa lettre ; le tirage est fait.
func (e *env) twoTargetsGroup(t *testing.T, slug string) (gid int64, all []int64) {
	t.Helper()
	gid = e.group(t, slug)
	all = append(all, e.addN(t, gid, "Bertille", 2, participants.RoleReceptionSeule)...)
	all = append(all, e.addN(t, gid, "Evariste", 2, participants.RoleOffrePlus)...)
	all = append(all, e.addN(t, gid, "Mederic", 2, participants.RoleNormal)...)
	e.write(t, gid, all...)
	e.mustDraw(t, gid)
	return gid, all
}

func drawReq() draw.Request { return draw.Request{Confirmed: true} }
