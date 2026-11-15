package enrolment

import (
	"bytes"
	"database/sql"
	"image/png"
	"testing"
	"time"

	"github.com/makiuchi-d/gozxing"
	"github.com/makiuchi-d/gozxing/qrcode"

	"secretsanta/internal/access/migrations"
	"secretsanta/internal/access/sqlstore"
	"secretsanta/internal/access/token"
	"secretsanta/internal/db"
	"secretsanta/internal/groups"
	"secretsanta/internal/participants"
	"secretsanta/internal/shared/clock"
	"secretsanta/internal/shared/ports"
)

var t0 = time.Date(2026, 11, 15, 10, 0, 0, 0, time.UTC)

type env struct {
	db    *sql.DB
	parts *participants.Service
	svc   *Service
	gid   int64
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
	am, _ := migrations.Migrations()
	for mod, ms := range map[string][]db.Migration{groups.Module: gm} {
		if err := db.Migrate(d, mod, ms); err != nil {
			t.Fatal(err)
		}
	}
	if err := db.Migrate(d, participants.Module, pm); err != nil {
		t.Fatal(err)
	}
	if err := db.Migrate(d, migrations.Module, am); err != nil {
		t.Fatal(err)
	}
	res, err := d.Exec(`INSERT INTO groups (slug, name, admin_secret_hash, created_at) VALUES ('noel', 'G', zeroblob(32), ?)`, t0.Format(time.RFC3339))
	if err != nil {
		t.Fatal(err)
	}
	gid, _ := res.LastInsertId()
	tk, err := token.NewService(sqlstore.New(d), token.LoginCodeLength, []byte("cle-de-test-0123456789-0123456789"))
	if err != nil {
		t.Fatal(err)
	}
	ps := participants.NewService(d, clock.NewFake(t0), &ports.FakeDrawState{Done: map[int64]bool{}})
	link := func(slug, tok string) string { return "https://exemple.test/" + slug + "/acces/" + tok }
	return &env{db: d, parts: ps, svc: NewService(d, ps, tk, link), gid: gid}
}

// decodeQR décode un PNG de QR code et renvoie le texte.
func decodeQR(t *testing.T, data []byte) string {
	t.Helper()
	img, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("PNG invalide : %v", err)
	}
	bmp, err := gozxing.NewBinaryBitmapFromImage(img)
	if err != nil {
		t.Fatal(err)
	}
	r, err := qrcode.NewQRCodeReader().Decode(bmp, nil)
	if err != nil {
		t.Fatalf("QR non décodable : %v", err)
	}
	return r.GetText()
}
