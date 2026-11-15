package reminders

import (
	"context"
	"testing"
	"time"

	"secretsanta/internal/db"
	"secretsanta/internal/draw"
	"secretsanta/internal/groups"
	"secretsanta/internal/letter"
	"secretsanta/internal/participants"
	"secretsanta/internal/shared/clock"
)

func TestSQLFactsReadRealTables(t *testing.T) {
	d, err := db.Open(t.TempDir())
	must(t, err)
	t.Cleanup(func() { _ = d.Close() })
	for _, m := range []struct {
		mod string
		fn  func() ([]db.Migration, error)
	}{{groups.Module, groups.Migrations}, {participants.Module, participants.Migrations},
		{letter.Module, letter.Migrations}, {draw.Module, draw.Migrations}, {Module, Migrations}} {
		ms, err := m.fn()
		must(t, err)
		must(t, db.Migrate(d, m.mod, ms))
	}
	now := time.Date(2026, 11, 5, 9, 0, 0, 0, time.UTC)
	f := SQLFacts{DB: d, Clock: clock.NewFake(now)}
	ctx := context.Background()
	_, err = d.Exec(`INSERT INTO groups (id, slug, name, admin_secret_hash, created_at) VALUES (1, 'groupe', 'G', zeroblob(32), ?)`, now.Format(time.RFC3339))
	must(t, err)
	for _, id := range []int{1, 2, 3} {
		_, err = d.Exec(`INSERT INTO participants (id, group_id, name, first_name, name_key, joined_at) VALUES (?, 1, ?, ?, ?, ?)`,
			id, string(rune('A'+id)), string(rune('A'+id)), string(rune('a'+id)), now.Format(time.RFC3339))
		must(t, err)
	}

	if ok, _ := f.LinkOpened(ctx, 1, 1); ok {
		t.Fatal("lien ouvert à tort")
	}
	must(t, f.RecordLinkOpened(ctx, 1, 1))
	must(t, f.RecordLinkOpened(ctx, 1, 1))
	if ok, _ := f.LinkOpened(ctx, 1, 1); !ok {
		t.Fatal("lien ouvert non vu")
	}

	if ok, _ := f.LetterWritten(ctx, 1, 2); ok {
		t.Fatal("lettre écrite à tort")
	}
	_, err = d.Exec(`INSERT INTO letters (group_id, participant_id, created_at, updated_at) VALUES (1, 2, 'x', 'x')`)
	must(t, err)
	if ok, _ := f.LetterWritten(ctx, 1, 2); !ok {
		t.Fatal("lettre écrite non vue")
	}

	_, err = d.Exec(`INSERT INTO draws (id, group_id, year, kind, status, gift_cap, drawn_at) VALUES (1, 1, 2026, 'tirage', 'actuel', 2, 'x')`)
	must(t, err)
	_, err = d.Exec(`INSERT INTO draw_pairs (draw_id, giver_id, receiver_id, discovered_at) VALUES (1, 1, 2, NULL), (1, 2, 3, 'x')`)
	must(t, err)
	got, err := f.UnreadGivers(ctx, 1)
	must(t, err)
	if len(got) != 1 || !got[1] {
		t.Fatalf("tireurs en retard : %v", got)
	}
}
