package repair

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

	"secretsanta/internal/adjust"
	"secretsanta/internal/config"
	"secretsanta/internal/db"
	"secretsanta/internal/draw"
	"secretsanta/internal/groups"
	"secretsanta/internal/participants"
	"secretsanta/internal/rulechange"
	"secretsanta/internal/rules"
	"secretsanta/internal/shared/clock"
	"secretsanta/internal/shared/ports"
)

var t0 = time.Date(2026, 11, 15, 10, 0, 0, 0, time.UTC)

var ctx = context.Background()

type Pair = adjust.Pair

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

type archiveCall struct {
	Giver, OldReceiver int64
	InTx               bool
}

type fakeArchiver struct {
	mu    sync.Mutex
	calls []archiveCall
	err   error
}

func (f *fakeArchiver) ArchiveTx(_ context.Context, tx *sql.Tx, _, giver, old int64) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return f.err
	}
	f.calls = append(f.calls, archiveCall{giver, old, tx != nil})
	return nil
}

type env struct {
	db       *sql.DB
	draw     *draw.Service
	parts    *participants.Service
	rules    *rules.Service
	mail     *ports.FakeEmailSender
	archiver *fakeArchiver
	svc      *Service
	rc       *rulechange.Service
	gid      int64
	names    []string
	emails   map[int64]string
	firsts   map[int64]string
}

// newEnv crée un groupe (plafond capN, 0 = 2) et branche le vrai service de
// règles (rulechange, F3d) sur ce port de réparation.
func newEnv(t *testing.T, capN int) *env {
	t.Helper()
	d, err := db.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = d.Close() })
	steps := []func() (string, []db.Migration, error){
		func() (string, []db.Migration, error) { m, err := groups.Migrations(); return groups.Module, m, err },
		func() (string, []db.Migration, error) {
			m, err := participants.Migrations()
			return participants.Module, m, err
		},
		func() (string, []db.Migration, error) { m, err := rules.Migrations(); return rules.Module, m, err },
		func() (string, []db.Migration, error) { m, err := draw.Migrations(); return draw.Module, m, err },
		func() (string, []db.Migration, error) {
			m, err := rulechange.Migrations()
			return rulechange.Module, m, err
		},
	}
	for _, s := range steps {
		module, ms, err := s()
		if err != nil {
			t.Fatal(err)
		}
		if err := db.Migrate(d, module, ms); err != nil {
			t.Fatal(err)
		}
	}
	c := clock.NewFake(t0)
	g, err := groups.NewService(d, c, config.NewSecret("mot-de-passe-instance"))
	if err != nil {
		t.Fatal(err)
	}
	if capN == 0 {
		capN = 2
	}
	res, err := d.Exec(`INSERT INTO groups (slug, name, admin_secret_hash, created_at, max_gifts_per_giver)
		VALUES ('famille', 'Famille', zeroblob(32), ?, ?)`, t0.Format(time.RFC3339Nano), capN)
	if err != nil {
		t.Fatal(err)
	}
	gid, _ := res.LastInsertId()
	e := &env{db: d, gid: gid, mail: &ports.FakeEmailSender{}, archiver: &fakeArchiver{},
		emails: map[int64]string{}, firsts: map[int64]string{}}
	e.parts = participants.NewService(d, c, draw.NewState(d))
	e.rules = rules.NewService(d, sqlDirectory{db: d})
	e.draw = draw.NewService(d, c, g, e.parts, e.rules, nil, draw.Options{})
	e.svc = NewService(d, Deps{
		Draw: e.draw, Groups: g, Roster: e.parts, Constraints: e.rules,
		Directory: adjust.ParticipantDirectory{Participants: e.parts}, Mail: e.mail,
		Archiver: e.archiver,
	})
	e.rc = rulechange.NewService(d, c, draw.NewState(d), e.svc)
	return e
}

func (e *env) add(t *testing.T, first string, role participants.DrawRole) int64 {
	t.Helper()
	name := "Zx" + first + "ovitch"
	email := strings.ToLower(first) + "@exemple.fr"
	p, err := e.parts.Create(ctx, participants.AdminActor(e.gid), e.gid,
		participants.Input{FirstName: first, Name: name, Email: email, Role: role})
	if err != nil {
		t.Fatalf("ajout de %s : %v", first, err)
	}
	e.names = append(e.names, first, name)
	e.emails[p.ID] = email
	e.firsts[p.ID] = first
	return p.ID
}

func (e *env) people(t *testing.T, roles ...participants.DrawRole) []int64 {
	t.Helper()
	var ids []int64
	for i, r := range roles {
		ids = append(ids, e.add(t, fmt.Sprintf("Perso%c", 'A'+i), r))
	}
	return ids
}

// setPairs fait le tirage puis impose les paires données.
func (e *env) setPairs(t *testing.T, pairs []Pair) {
	t.Helper()
	if _, err := e.draw.Draw(ctx, participants.AdminActor(e.gid), e.gid, draw.Request{Confirmed: true, CapConfirmed: true}); err != nil {
		t.Fatalf("tirage refusé : %v", err)
	}
	tx, err := e.db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	out := make([]draw.Pair, len(pairs))
	for i, p := range pairs {
		out[i] = draw.Pair{Giver: p.Giver, Receiver: p.Receiver}
	}
	if err := e.draw.AdjustTx(ctx, tx, e.gid, out); err != nil {
		_ = tx.Rollback()
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
}

func ring(ids ...int64) []Pair {
	var out []Pair
	for i := range ids {
		out = append(out, Pair{Giver: ids[i], Receiver: ids[(i+1)%len(ids)]})
	}
	return out
}

func (e *env) drawRow(t *testing.T) (int64, int) {
	t.Helper()
	var id int64
	var rev int
	if err := e.db.QueryRow(`SELECT id, revision FROM draws WHERE group_id = ? AND status = 'actuel'`, e.gid).Scan(&id, &rev); err != nil {
		t.Fatal(err)
	}
	return id, rev
}

func (e *env) discover(t *testing.T, giver, receiver int64) {
	t.Helper()
	id, _ := e.drawRow(t)
	r, err := e.db.Exec(`UPDATE draw_pairs SET discovered_at = ? WHERE draw_id = ? AND giver_id = ? AND receiver_id = ?`,
		t0.Format(time.RFC3339Nano), id, giver, receiver)
	if err != nil {
		t.Fatal(err)
	}
	if n, _ := r.RowsAffected(); n != 1 {
		t.Fatalf("paire %d→%d absente", giver, receiver)
	}
}

type row struct {
	Giver, Receiver int64
	Discovered      sql.NullString
}

func (e *env) rows(t *testing.T) []row {
	t.Helper()
	id, _ := e.drawRow(t)
	rs, err := e.db.Query(`SELECT giver_id, receiver_id, discovered_at FROM draw_pairs WHERE draw_id = ? ORDER BY giver_id, receiver_id`, id)
	if err != nil {
		t.Fatal(err)
	}
	defer rs.Close()
	var out []row
	for rs.Next() {
		var r row
		if err := rs.Scan(&r.Giver, &r.Receiver, &r.Discovered); err != nil {
			t.Fatal(err)
		}
		out = append(out, r)
	}
	return out
}

func (e *env) pairs(t *testing.T) []Pair {
	t.Helper()
	var out []Pair
	for _, r := range e.rows(t) {
		out = append(out, Pair{Giver: r.Giver, Receiver: r.Receiver})
	}
	return out
}

func sorted(p []Pair) []Pair {
	out := append([]Pair(nil), p...)
	sort.Slice(out, func(i, j int) bool {
		if out[i].Giver != out[j].Giver {
			return out[i].Giver < out[j].Giver
		}
		return out[i].Receiver < out[j].Receiver
	})
	return out
}

func samePairSet(t *testing.T, got, want []Pair) {
	t.Helper()
	if !reflect.DeepEqual(sorted(got), sorted(want)) {
		t.Fatalf("paires %v, attendu %v", sorted(got), sorted(want))
	}
}

func changedCount(before, after []Pair) int {
	set := map[Pair]bool{}
	for _, p := range before {
		set[p] = true
	}
	n := 0
	for _, p := range after {
		if !set[p] {
			n++
		}
	}
	return n
}

func (e *env) count(t *testing.T, q string, args ...any) int {
	t.Helper()
	var n int
	if err := e.db.QueryRow(q, args...).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func (e *env) roleOf(t *testing.T, id int64) participants.DrawRole {
	t.Helper()
	var r string
	if err := e.db.QueryRow(`SELECT draw_role FROM participants WHERE id = ?`, id).Scan(&r); err != nil {
		t.Fatal(err)
	}
	return participants.DrawRole(r)
}

func (e *env) roles(t *testing.T, ids []int64) map[int64]participants.DrawRole {
	t.Helper()
	out := map[int64]participants.DrawRole{}
	for _, id := range ids {
		out[id] = e.roleOf(t, id)
	}
	return out
}

// previewCountsOnly vérifie que l'aperçu ne porte que des comptes et un
// booléen : aucun champ texte ni identifiant ne peut y glisser un nom.
func previewCountsOnly(t *testing.T, p ports.RepairPreview) {
	t.Helper()
	ty := reflect.TypeOf(p)
	for i := 0; i < ty.NumField(); i++ {
		switch ty.Field(i).Type.Kind() {
		case reflect.Int, reflect.Bool:
		default:
			t.Fatalf("champ %s de type %s dans l'aperçu", ty.Field(i).Name, ty.Field(i).Type)
		}
	}
}

// checkMails vérifie que seules les personnes attendues ont reçu un
// courriel, et que chacun ne nomme que son destinataire.
func (e *env) checkMails(t *testing.T, want []int64) {
	t.Helper()
	var got, exp []string
	for _, m := range e.mail.Sent {
		got = append(got, m.To)
		var me int64
		for id, addr := range e.emails {
			if addr == m.To {
				me = id
			}
		}
		if me == 0 {
			t.Fatalf("courriel à une adresse inconnue %q", m.To)
		}
		low := strings.ToLower(m.Subject + "\n" + m.Body)
		for id, first := range e.firsts {
			if id == me {
				continue
			}
			if strings.Contains(low, strings.ToLower(first)) || strings.Contains(low, strings.ToLower("Zx"+first+"ovitch")) {
				t.Fatalf("le courriel à %q nomme une autre personne : %q", m.To, m.Body)
			}
		}
	}
	for _, id := range want {
		exp = append(exp, e.emails[id])
	}
	sort.Strings(got)
	sort.Strings(exp)
	if !reflect.DeepEqual(got, exp) {
		t.Fatalf("courriels à %v, attendu %v", got, exp)
	}
}

// givers renvoie les tireurs dont les paires diffèrent entre before et after.
func givers(before, after []Pair) []int64 {
	b, a := map[Pair]bool{}, map[Pair]bool{}
	for _, p := range before {
		b[p] = true
	}
	for _, p := range after {
		a[p] = true
	}
	set := map[int64]bool{}
	for _, p := range before {
		if !a[p] {
			set[p.Giver] = true
		}
	}
	for _, p := range after {
		if !b[p] {
			set[p.Giver] = true
		}
	}
	var out []int64
	for id := range set {
		out = append(out, id)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

// checkValid vérifie la structure du tirage avec les rôles et le plafond.
func checkValid(t *testing.T, pairs []Pair, roles map[int64]participants.DrawRole, capN int) {
	t.Helper()
	recv, give := map[int64]int{}, map[int64]int{}
	for _, p := range pairs {
		if p.Giver == p.Receiver {
			t.Fatalf("%d s'offre son propre cadeau", p.Giver)
		}
		recv[p.Receiver]++
		give[p.Giver]++
	}
	for id, role := range roles {
		if recv[id] != 1 {
			t.Fatalf("%d reçoit %d cadeaux", id, recv[id])
		}
		lo, hi := 1, capN
		switch role {
		case participants.RoleReceptionSeule:
			lo, hi = 0, 0
		case participants.RoleUnSeul:
			lo, hi = 1, 1
		}
		if give[id] < lo || give[id] > hi {
			t.Fatalf("%d (%s) offre %d cadeaux", id, role, give[id])
		}
	}
}

// bruteOptimum énumère tous les tirages valides et renvoie le meilleur
// (cibles découvertes perdues, paires changées), dans cet ordre.
func bruteOptimum(ids []int64, roles map[int64]participants.DrawRole, capN int, excl, forced map[Pair]bool,
	current []Pair, discovered map[Pair]bool) (revealedLost, changed int, ok bool) {
	cur := map[Pair]bool{}
	for _, p := range current {
		cur[p] = true
	}
	n := len(ids)
	choice := make([]int, n)
	best := [2]int{1 << 30, 1 << 30}
	var rec func(i int)
	rec = func(i int) {
		if i == n {
			give := map[int64]int{}
			var ps []Pair
			for r, g := range choice {
				p := Pair{Giver: ids[g], Receiver: ids[r]}
				give[p.Giver]++
				ps = append(ps, p)
			}
			for _, id := range ids {
				lo, hi := 1, capN
				switch roles[id] {
				case participants.RoleReceptionSeule:
					lo, hi = 0, 0
				case participants.RoleUnSeul:
					lo, hi = 1, 1
				}
				if give[id] < lo || give[id] > hi {
					return
				}
			}
			next := map[Pair]bool{}
			for _, p := range ps {
				next[p] = true
			}
			for f := range forced {
				if !next[f] {
					return
				}
			}
			lostRev := map[int64]bool{}
			for p := range discovered {
				if discovered[p] && !next[p] {
					lostRev[p.Giver] = true
				}
			}
			c := 0
			for _, p := range ps {
				if !cur[p] {
					c++
				}
			}
			k := [2]int{len(lostRev), c}
			if k[0] < best[0] || (k[0] == best[0] && k[1] < best[1]) {
				best = k
			}
			ok = true
			return
		}
		for g := 0; g < n; g++ {
			if g == i || excl[Pair{Giver: ids[g], Receiver: ids[i]}] {
				continue
			}
			choice[i] = g
			rec(i + 1)
		}
	}
	rec(0)
	return best[0], best[1], ok
}
