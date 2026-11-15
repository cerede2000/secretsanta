package adminops_test

import (
	"context"
	"database/sql"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"secretsanta/internal/adminops"
	"secretsanta/internal/app"
	"secretsanta/internal/config"
	"secretsanta/internal/db"
	"secretsanta/internal/draw"
	"secretsanta/internal/groups"
	"secretsanta/internal/participants"
	"secretsanta/internal/shared/clock"
)

const instancePass = "mot-de-passe-instance-de-test"

var t0 = time.Date(2026, 11, 15, 10, 0, 0, 0, time.UTC)

type fixture struct {
	db     *sql.DB
	clock  *clock.Fake
	groups *groups.Service
	people *participants.Service
	ops    *adminops.Service
	images string
}

// newFixture ouvre une base neuve avec le registre complet des migrations de
// l'application, plus celles du module (pas encore enregistrées par
// l'orchestrateur ; rejouer une migration déjà appliquée est sans effet).
func newFixture(t *testing.T) *fixture {
	t.Helper()
	d, err := db.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = d.Close() })
	if err := app.Migrate(d); err != nil {
		t.Fatal(err)
	}
	ms, err := adminops.Migrations()
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Migrate(d, adminops.Module, ms); err != nil {
		t.Fatal(err)
	}
	clk := clock.NewFake(t0)
	gs, err := groups.NewService(d, clk, config.NewSecret(instancePass))
	if err != nil {
		t.Fatal(err)
	}
	images := t.TempDir()
	ops, err := adminops.NewService(d, clk, adminops.Options{ImageDir: images, TempDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	return &fixture{db: d, clock: clk, groups: gs, people: participants.NewService(d, clk, draw.NewState(d)), ops: ops, images: images}
}

// group crée un groupe et renvoie son identifiant et son secret.
func (f *fixture) group(t *testing.T, slug, name string, eventDate string) (int64, string) {
	t.Helper()
	res, err := f.groups.Create(context.Background(), groups.CreateInput{
		InstancePassword: instancePass, ClientKey: "test", Name: name, Slug: slug,
		Budget: "25", GiftInstructions: "Emballé", EventDate: eventDate,
	})
	if err != nil {
		t.Fatalf("création du groupe %s : %v", slug, err)
	}
	return res.Group.ID, res.AdminSecret
}

func (f *fixture) person(t *testing.T, gid int64, first, name, email string, role participants.DrawRole) int64 {
	t.Helper()
	p, err := f.people.Create(context.Background(), participants.AdminActor(gid), gid, participants.Input{
		FirstName: first, Name: name, Email: email, GiftHint: "Livres", Role: role,
	})
	if err != nil {
		t.Fatalf("participant %s %s : %v", first, name, err)
	}
	return p.ID
}

func (f *fixture) exec(t *testing.T, q string, args ...any) int64 {
	t.Helper()
	res, err := f.db.Exec(q, args...)
	if err != nil {
		t.Fatalf("%s : %v", q, err)
	}
	id, _ := res.LastInsertId()
	return id
}

func (f *fixture) count(t *testing.T, q string, args ...any) int {
	t.Helper()
	var n int
	if err := f.db.QueryRow(q, args...).Scan(&n); err != nil {
		t.Fatalf("%s : %v", q, err)
	}
	return n
}

func (f *fixture) household(t *testing.T, gid int64, name string, members ...int64) int64 {
	t.Helper()
	hid := f.exec(t, `INSERT INTO households(group_id, name) VALUES (?, ?)`, gid, name)
	for _, m := range members {
		f.exec(t, `INSERT INTO household_members(participant_id, household_id) VALUES (?, ?)`, m, hid)
	}
	return hid
}

// seedDraw pose un tirage actuel avec des paires et une lettre par personne.
func (f *fixture) seedDrawAndLetters(t *testing.T, gid int64, ids []int64) {
	t.Helper()
	did := f.exec(t, `INSERT INTO draws(group_id, year, kind, status, gift_cap, drawn_at) VALUES (?, 2026, 'tirage', 'actuel', 1, ?)`,
		gid, t0.Format(time.RFC3339))
	for i, id := range ids {
		f.exec(t, `INSERT INTO draw_pairs(draw_id, giver_id, receiver_id) VALUES (?, ?, ?)`, did, id, ids[(i+1)%len(ids)])
		lid := f.exec(t, `INSERT INTO letters(group_id, participant_id, intro, created_at, updated_at) VALUES (?, ?, 'Cher Père Noël', ?, ?)`,
			gid, id, t0.Format(time.RFC3339), t0.Format(time.RFC3339))
		f.exec(t, `INSERT INTO letter_ideas(letter_id, position, title) VALUES (?, 1, 'Un livre')`, lid)
	}
}

// seedTraces pose des lignes rattachées au groupe hors clés étrangères.
func (f *fixture) seedTraces(t *testing.T, gid int64, pid int64) {
	t.Helper()
	g := strconv.FormatInt(gid, 10)
	now := t0.UnixNano()
	f.exec(t, `INSERT INTO mail_queue(group_id, participant_id, recipient, mail_type, status, created_at, updated_at, send_after) VALUES (?, ?, 'a@exemple.fr', 'invitation', 'sent', ?, ?, ?)`, gid, pid, now, now, now)
	f.exec(t, `INSERT INTO mail_queue(group_id, participant_id, recipient, mail_type, status, created_at, updated_at, send_after) VALUES (?, ?, 'a@exemple.fr', 'invitation', 'pending', ?, ?, ?)`, gid, pid, now, now, now)
	f.exec(t, `INSERT INTO mail_suppression(group_id, address, created_at) VALUES (?, ?, ?)`, gid, "rebond-"+g+"@exemple.fr", now)
	f.exec(t, `INSERT INTO mail_unsubscribe(participant_id, mail_type, group_id, created_at) VALUES (?, 'relances', ?, ?)`, pid, gid, now)
	f.exec(t, `INSERT INTO magic_links(token_hash, group_id, participant_id, created_at, expires_at) VALUES (?, ?, ?, ?, ?)`, []byte("jeton-"+g), gid, pid, now, now+1)
	f.exec(t, `INSERT INTO access_credentials(group_id, participant_id, kind, token_hash) VALUES (?, ?, 'link', ?)`, g, strconv.FormatInt(pid, 10), strings.Repeat(string("abcdef0123456789"[gid%16]), 64))
	f.exec(t, `INSERT INTO websec_sessions(id_hash, kind, group_id, subject, created_at, expires_at) VALUES (?, 'admin', ?, '', ?, ?)`, "session-"+g, g, now, now+int64(time.Hour))
}

type person struct {
	First, Name, Email, Hint, Role, Locale string
}

// roster renvoie les participants du groupe, triés par nom.
func roster(t *testing.T, d *sql.DB, gid int64) []person {
	t.Helper()
	rows, err := d.Query(`SELECT first_name, name, email, gift_hint, draw_role, locale FROM participants WHERE group_id = ?`, gid)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []person
	for rows.Next() {
		var p person
		if err := rows.Scan(&p.First, &p.Name, &p.Email, &p.Hint, &p.Role, &p.Locale); err != nil {
			t.Fatal(err)
		}
		out = append(out, p)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].First+out[i].Name < out[j].First+out[j].Name })
	return out
}

// households renvoie les foyers sous forme « nom: prénom nom, … » triés.
func households(t *testing.T, d *sql.DB, gid int64) []string {
	t.Helper()
	rows, err := d.Query(`SELECT h.id, h.name, p.first_name || ' ' || p.name FROM households h
		JOIN household_members m ON m.household_id = h.id JOIN participants p ON p.id = m.participant_id
		WHERE h.group_id = ? ORDER BY h.id`, gid)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	byID := map[int64][]string{}
	names := map[int64]string{}
	for rows.Next() {
		var id int64
		var hn, pn string
		if err := rows.Scan(&id, &hn, &pn); err != nil {
			t.Fatal(err)
		}
		names[id] = hn
		byID[id] = append(byID[id], pn)
	}
	var out []string
	for id, ms := range byID {
		sort.Strings(ms)
		out = append(out, names[id]+": "+strings.Join(ms, ", "))
	}
	sort.Strings(out)
	return out
}

// namedPairs renvoie les couples d'une table de règles, par noms, triés.
func namedPairs(t *testing.T, d *sql.DB, table string, gid int64) []string {
	t.Helper()
	rows, err := d.Query(`SELECT a.first_name, b.first_name FROM `+table+` x
		JOIN participants a ON a.id = x.giver_id JOIN participants b ON b.id = x.receiver_id WHERE x.group_id = ?`, gid)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var a, b string
		if err := rows.Scan(&a, &b); err != nil {
			t.Fatal(err)
		}
		out = append(out, a+">"+b)
	}
	sort.Strings(out)
	return out
}

// groupRowCounts compte, pour chaque table portant group_id, les lignes du
// groupe (et la ligne groups elle-même).
func groupRowCounts(t *testing.T, d *sql.DB, gid int64) map[string]int {
	t.Helper()
	rows, err := d.Query(`SELECT m.name FROM sqlite_master m WHERE m.type = 'table' AND m.name NOT LIKE 'sqlite_%'
		AND EXISTS (SELECT 1 FROM pragma_table_info(m.name) c WHERE c.name = 'group_id')`)
	if err != nil {
		t.Fatal(err)
	}
	var tables []string
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			t.Fatal(err)
		}
		tables = append(tables, n)
	}
	rows.Close()
	out := map[string]int{}
	for _, tb := range tables {
		var n int
		if err := d.QueryRow(`SELECT COUNT(*) FROM "`+tb+`" WHERE CAST(group_id AS TEXT) = ?`, strconv.FormatInt(gid, 10)).Scan(&n); err != nil {
			t.Fatal(err)
		}
		out[tb] = n
	}
	var n int
	if err := d.QueryRow(`SELECT COUNT(*) FROM groups WHERE id = ?`, gid).Scan(&n); err != nil {
		t.Fatal(err)
	}
	out["groups"] = n
	return out
}

func roleOf(r string) *string { return &r }
