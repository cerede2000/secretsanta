package adjust

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

	"secretsanta/internal/config"
	"secretsanta/internal/db"
	"secretsanta/internal/draw"
	"secretsanta/internal/groups"
	"secretsanta/internal/participants"
	"secretsanta/internal/rules"
	"secretsanta/internal/shared/clock"
	"secretsanta/internal/shared/errs"
	"secretsanta/internal/shared/ports"
)

var t0 = time.Date(2026, 11, 15, 10, 0, 0, 0, time.UTC)

var ctx = context.Background()

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

// archiveCall est un archivage reçu par la doublure.
type archiveCall struct {
	GroupID, Giver, OldReceiver int64
	InTx                        bool
}

type fakeArchiver struct {
	mu    sync.Mutex
	calls []archiveCall
	err   error
}

func (f *fakeArchiver) ArchiveTx(_ context.Context, tx *sql.Tx, groupID, giver, old int64) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return f.err
	}
	f.calls = append(f.calls, archiveCall{groupID, giver, old, tx != nil})
	return nil
}

// sqlRemover est la doublure du retrait : elle supprime la ligne du
// participant et les règles qui le citent, dans la transaction reçue.
type sqlRemover struct {
	calls []int64
}

func (r *sqlRemover) RemoveTx(ctx context.Context, tx *sql.Tx, groupID, id int64) error {
	for _, q := range []string{
		`DELETE FROM household_members WHERE participant_id = ?1 AND household_id IN (SELECT id FROM households WHERE group_id = ?2)`,
		`DELETE FROM manual_exclusions WHERE (giver_id = ?1 OR receiver_id = ?1) AND group_id = ?2`,
		`DELETE FROM forced_pairs WHERE (giver_id = ?1 OR receiver_id = ?1) AND group_id = ?2`,
		`DELETE FROM participants WHERE id = ?1 AND group_id = ?2`,
	} {
		if _, err := tx.ExecContext(ctx, q, id, groupID); err != nil {
			return err
		}
	}
	r.calls = append(r.calls, id)
	return nil
}

type env struct {
	db       *sql.DB
	clock    *clock.Fake
	groups   *groups.Service
	parts    *participants.Service
	rules    *rules.Service
	draw     *draw.Service
	mail     *ports.FakeEmailSender
	archiver *fakeArchiver
	remover  *sqlRemover
	svc      *Service
	names    []string
	emails   map[int64]string
	firsts   map[int64]string
}

func migrateAll(t *testing.T, d *sql.DB, extra ...func() (string, []db.Migration, error)) {
	t.Helper()
	steps := []func() (string, []db.Migration, error){
		func() (string, []db.Migration, error) { m, err := groups.Migrations(); return groups.Module, m, err },
		func() (string, []db.Migration, error) {
			m, err := participants.Migrations()
			return participants.Module, m, err
		},
		func() (string, []db.Migration, error) { m, err := rules.Migrations(); return rules.Module, m, err },
		func() (string, []db.Migration, error) { m, err := draw.Migrations(); return draw.Module, m, err },
	}
	for _, s := range append(steps, extra...) {
		module, ms, err := s()
		if err != nil {
			t.Fatal(err)
		}
		if err := db.Migrate(d, module, ms); err != nil {
			t.Fatal(err)
		}
	}
}

func newEnv(t *testing.T, extra ...func() (string, []db.Migration, error)) *env {
	t.Helper()
	d, err := db.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = d.Close() })
	migrateAll(t, d, extra...)
	c := clock.NewFake(t0)
	g, err := groups.NewService(d, c, config.NewSecret("mot-de-passe-instance"))
	if err != nil {
		t.Fatal(err)
	}
	e := &env{db: d, clock: c, groups: g, mail: &ports.FakeEmailSender{}, archiver: &fakeArchiver{},
		remover: &sqlRemover{}, emails: map[int64]string{}, firsts: map[int64]string{}}
	e.parts = participants.NewService(d, c, draw.NewState(d))
	e.rules = rules.NewService(d, sqlDirectory{db: d})
	e.draw = draw.NewService(d, c, g, e.parts, e.rules, nil, draw.Options{})
	e.svc = NewService(d, Deps{
		Draw: e.draw, Groups: g, Roster: e.parts, Constraints: e.rules,
		Directory: ParticipantDirectory{Participants: e.parts}, Mail: e.mail,
		Archiver: e.archiver, Remover: e.remover,
	})
	return e
}

// group crée un groupe ; maxGifts 0 garde la valeur par défaut (2).
func (e *env) group(t *testing.T, slug string, maxGifts int) int64 {
	t.Helper()
	if maxGifts == 0 {
		maxGifts = 2
	}
	res, err := e.db.Exec(`INSERT INTO groups (slug, name, admin_secret_hash, created_at, max_gifts_per_giver)
		VALUES (?, 'Famille', zeroblob(32), ?, ?)`, slug, t0.Format(time.RFC3339Nano), maxGifts)
	if err != nil {
		t.Fatal(err)
	}
	id, _ := res.LastInsertId()
	return id
}

// add ajoute un participant au nom reconnaissable.
func (e *env) add(t *testing.T, gid int64, first string, role participants.DrawRole) int64 {
	t.Helper()
	name := "Zx" + first + "ovitch"
	email := strings.ToLower(first) + "@exemple.fr"
	p, err := e.parts.Create(ctx, participants.AdminActor(gid), gid,
		participants.Input{FirstName: first, Name: name, Email: email, Role: role})
	if err != nil {
		t.Fatalf("ajout de %s : %v", first, err)
	}
	e.names = append(e.names, first, name)
	e.emails[p.ID] = email
	e.firsts[p.ID] = first
	return p.ID
}

func (e *env) people(t *testing.T, gid int64, roles ...participants.DrawRole) []int64 {
	t.Helper()
	var ids []int64
	for i, r := range roles {
		ids = append(ids, e.add(t, gid, fmt.Sprintf("Perso%c", 'A'+i), r))
	}
	return ids
}

func admin(gid int64) participants.Actor { return participants.AdminActor(gid) }

// setPairs fait le tirage puis impose les paires données (vérité du test).
func (e *env) setPairs(t *testing.T, gid int64, pairs []Pair) {
	t.Helper()
	if _, err := e.draw.Draw(ctx, admin(gid), gid, draw.Request{Confirmed: true, CapConfirmed: true}); err != nil {
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
	if err := e.draw.AdjustTx(ctx, tx, gid, out); err != nil {
		_ = tx.Rollback()
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
}

// ring renvoie le cycle ids[0]→ids[1]→…→ids[0].
func ring(ids ...int64) []Pair {
	var out []Pair
	for i := range ids {
		out = append(out, Pair{ids[i], ids[(i+1)%len(ids)]})
	}
	return out
}

func (e *env) drawID(t *testing.T, gid int64) int64 {
	t.Helper()
	var id int64
	if err := e.db.QueryRow(`SELECT id FROM draws WHERE group_id = ? AND status = 'actuel'`, gid).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}

// discover pose le drapeau de découverte de la paire giver→receiver.
func (e *env) discover(t *testing.T, gid, giver, receiver int64) {
	t.Helper()
	r, err := e.db.Exec(`UPDATE draw_pairs SET discovered_at = ? WHERE draw_id = ? AND giver_id = ? AND receiver_id = ?`,
		t0.Format(time.RFC3339Nano), e.drawID(t, gid), giver, receiver)
	if err != nil {
		t.Fatal(err)
	}
	if n, _ := r.RowsAffected(); n != 1 {
		t.Fatalf("paire %d→%d absente", giver, receiver)
	}
}

type row struct {
	ID         int64
	Giver      int64
	Receiver   int64
	Discovered sql.NullString
}

// rows lit les paires du tirage actuel (vérité de référence des tests).
func (e *env) rows(t *testing.T, gid int64) []row {
	t.Helper()
	rs, err := e.db.Query(`SELECT id, giver_id, receiver_id, discovered_at FROM draw_pairs WHERE draw_id = ? ORDER BY giver_id, receiver_id`,
		e.drawID(t, gid))
	if err != nil {
		t.Fatal(err)
	}
	defer rs.Close()
	var out []row
	for rs.Next() {
		var r row
		if err := rs.Scan(&r.ID, &r.Giver, &r.Receiver, &r.Discovered); err != nil {
			t.Fatal(err)
		}
		out = append(out, r)
	}
	return out
}

func (e *env) pairs(t *testing.T, gid int64) []Pair {
	t.Helper()
	var out []Pair
	for _, r := range e.rows(t, gid) {
		out = append(out, Pair{r.Giver, r.Receiver})
	}
	return out
}

func sorted(p []Pair) []Pair {
	out := append([]Pair(nil), p...)
	sortPairs(out)
	return out
}

func samePairSet(t *testing.T, got, want []Pair) {
	t.Helper()
	if !reflect.DeepEqual(sorted(got), sorted(want)) {
		t.Fatalf("paires %v, attendu %v", sorted(got), sorted(want))
	}
}

// changedCount compte les paires de after absentes de before.
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

// noNames vérifie qu'aucun nom ou prénom saisi n'apparaît dans s.
func (e *env) noNames(t *testing.T, s string) {
	t.Helper()
	low := strings.ToLower(s)
	for _, n := range e.names {
		if strings.Contains(low, strings.ToLower(n)) {
			t.Fatalf("un nom (%q) apparaît dans %q", n, s)
		}
	}
}

func (e *env) preview(t *testing.T, gid int64, ch Change) *Preview {
	t.Helper()
	p, err := e.svc.Preview(ctx, admin(gid), gid, ch)
	if err != nil {
		t.Fatalf("aperçu refusé : %v", err)
	}
	return p
}

func (e *env) apply(t *testing.T, gid int64, ch Change) *Result {
	t.Helper()
	p := e.preview(t, gid, ch)
	if !p.Possible {
		t.Fatalf("ajustement impossible : %s", p.Summary)
	}
	r, err := e.svc.Apply(ctx, admin(gid), gid, ch, Confirmation{Confirmed: true, Revision: p.Revision})
	if err != nil {
		t.Fatalf("ajustement refusé : %v", err)
	}
	return r
}

func remove(id int64) Change { return Change{Kind: KindRemove, ParticipantID: id} }
func addP(id int64) Change   { return Change{Kind: KindAdd, ParticipantID: id} }

// checkValid vérifie la structure d'un tirage : chaque personne reçoit
// exactement un cadeau, personne ne s'offre le sien, les rôles et le plafond
// sont respectés.
// fataler est satisfait par *testing.T et *rapid.T.
type fataler interface {
	Helper()
	Fatalf(format string, args ...any)
}

func checkValid(t fataler, pairs []Pair, roles map[int64]participants.DrawRole, capN int) {
	t.Helper()
	recv := map[int64]int{}
	give := map[int64]int{}
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
		switch role {
		case participants.RoleReceptionSeule:
			if give[id] != 0 {
				t.Fatalf("la réception seule %d offre %d cadeaux", id, give[id])
			}
		case participants.RoleUnSeul:
			if give[id] != 1 {
				t.Fatalf("le un_seul %d offre %d cadeaux", id, give[id])
			}
		default:
			if give[id] < 1 || give[id] > capN {
				t.Fatalf("%d offre %d cadeaux (plafond %d)", id, give[id], capN)
			}
		}
	}
	for id := range recv {
		if _, ok := roles[id]; !ok {
			t.Fatalf("%d n'est plus dans le tirage mais reçoit un cadeau", id)
		}
	}
	for id := range give {
		if _, ok := roles[id]; !ok {
			t.Fatalf("%d n'est plus dans le tirage mais offre un cadeau", id)
		}
	}
}

func receiversOf(pairs []Pair, giver int64) []int64 {
	var out []int64
	for _, p := range pairs {
		if p.Giver == giver {
			out = append(out, p.Receiver)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

func wantKind(t *testing.T, err error, k errs.Kind) {
	t.Helper()
	if err == nil || !errs.Is(err, k) {
		t.Fatalf("erreur %q attendue, obtenu %v", k, err)
	}
}
