package letter

import (
	"bytes"
	"context"
	"database/sql"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"strconv"
	"testing"
	"time"

	"secretsanta/internal/db"
	"secretsanta/internal/groups"
	"secretsanta/internal/participants"
	"secretsanta/internal/shared/clock"
	"secretsanta/internal/shared/errs"
	"secretsanta/internal/shared/ports"
)

var t0 = time.Date(2026, 11, 15, 10, 0, 0, 0, time.UTC)

type env struct {
	db    *sql.DB
	clock *clock.Fake
	bus   *ports.MemoryEventBus
	svc   *Service
	group int64
}

func newEnv(t *testing.T) *env {
	t.Helper()
	d, err := db.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = d.Close() })
	gm, _ := groups.Migrations()
	pm, _ := participants.Migrations()
	lm, err := Migrations()
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range []struct {
		n  string
		ms []db.Migration
	}{{groups.Module, gm}, {participants.Module, pm}, {Module, lm}} {
		if err := db.Migrate(d, m.n, m.ms); err != nil {
			t.Fatal(err)
		}
	}
	res, err := d.Exec(`INSERT INTO groups (slug, name, admin_secret_hash, created_at) VALUES ('groupe-test', 'G', zeroblob(32), ?)`, t0.Format(time.RFC3339))
	if err != nil {
		t.Fatal(err)
	}
	gid, _ := res.LastInsertId()
	c := clock.NewFake(t0)
	bus := &ports.MemoryEventBus{}
	return &env{db: d, clock: c, bus: bus, svc: NewService(d, c, bus, t.TempDir()), group: gid}
}

func (e *env) person(t *testing.T, first, role string) int64 {
	t.Helper()
	res, err := e.db.Exec(`INSERT INTO participants (group_id, name, first_name, name_key, joined_at, draw_role) VALUES (?, ?, ?, ?, ?, ?)`,
		e.group, first+" Test", first, first, t0.Format(time.RFC3339), role)
	if err != nil {
		t.Fatal(err)
	}
	id, _ := res.LastInsertId()
	return id
}

func (e *env) events() []ports.Event {
	var out []ports.Event
	for _, ev := range e.bus.Emitted {
		if ev.Name == EventUpdated {
			out = append(out, ev)
		}
	}
	return out
}

func pngBytes(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	img.Set(0, 0, color.RGBA{255, 0, 0, 255})
	var b bytes.Buffer
	if err := png.Encode(&b, img); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func wantKind(t *testing.T, err error, k errs.Kind) {
	t.Helper()
	if !errs.Is(err, k) {
		t.Fatalf("nature %s attendue, obtenu %v", k, err)
	}
}

func mustImg(t *testing.T) *image.RGBA {
	t.Helper()
	return image.NewRGBA(image.Rect(0, 0, 4, 4))
}

type context_ = context.Context

func fmtSscan(s string, n *int64) (int, error) { return fmt.Sscan(s, n) }

func lid2s(id int64) string { return strconv.FormatInt(id, 10) }
