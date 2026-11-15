package enrolment

import (
	"bytes"
	"database/sql"
	"image/png"
	"testing"
	"time"

	"github.com/makiuchi-d/gozxing"
	"github.com/makiuchi-d/gozxing/qrcode"
	"github.com/makiuchi-d/gozxing/qrcode/decoder"

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

// decodeQR décode un PNG de QR code et renvoie le texte, de façon
// déterministe : la grille de modules est lue directement dans l'image (le
// générateur dessine des carrés alignés) puis décodée avec le
// décodeur de gozxing (Reed-Solomon compris).
//
// Le détecteur d'image de gozxing (decodeQRDetected) n'est pas utilisé ici :
// il échoue sur environ 0,5 % des codes valides (mesuré : 15 sur 3000 liens de
// forme PUBLIC_URL/slug/acces/jeton aléatoire, quels que soient la taille et le
// niveau de correction), alors que le décodage de la même grille réussit
// toujours (0 échec sur 6000). Le jeton d'un lien personnel est aléatoire :
// avec le détecteur, TestPersonalLinkQRDecodesToSameLink échouait au hasard.
func decodeQR(t *testing.T, data []byte) string {
	t.Helper()
	img, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("PNG invalide : %v", err)
	}
	b := img.Bounds()
	dark := func(x, y int) bool {
		r, g, bl, _ := img.At(b.Min.X+x, b.Min.Y+y).RGBA()
		return (r+g+bl)/3 < 0x8000
	}
	w := b.Dx()
	// Largeur du motif de repérage (7 modules) : première ligne sombre en haut
	// à gauche ; avec la largeur de l'image on en déduit le nombre de modules
	// (zone de silence de 4 modules de chaque côté comprise), qui doit valoir
	// 17 + 4v + 8 pour une version v.
	x0, y0 := -1, -1
	for y := 0; y < w && x0 < 0; y++ {
		for x := 0; x < w; x++ {
			if dark(x, y) {
				x0, y0 = x, y
				break
			}
		}
	}
	if x0 < 0 {
		t.Fatal("QR code sans module sombre")
	}
	run := 0
	for x0+run < w && dark(x0+run, y0) {
		run++
	}
	total := int(float64(w)*7/float64(run) + 0.5) // modules, zone de silence comprise
	if dim := total - 8; dim < 21 || (dim-17)%4 != 0 {
		t.Fatalf("grille de %d modules : pas une version de QR code (largeur %d, repérage %d px)", total, w, run)
	}
	m := float64(w) / float64(total) // côté d'un module en pixels (pas toujours entier)
	n := total - 8
	grid := make([][]bool, n)
	for r := range grid {
		grid[r] = make([]bool, n)
		for c := range grid[r] {
			grid[r][c] = dark(int((4+float64(c)+0.5)*m), int((4+float64(r)+0.5)*m))
		}
	}
	res, err := decoder.NewDecoder().DecodeBoolMap(grid, nil)
	if err != nil {
		t.Fatalf("QR non décodable : %v", err)
	}
	return res.GetText()
}

// decodeQRDetected décode un PNG avec toute la chaîne de gozxing (détection du
// code dans l'image comprise), comme un lecteur d'images. Réservé aux entrées
// fixes : voir decodeQR pour la raison.
func decodeQRDetected(t *testing.T, data []byte) string {
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
		t.Fatalf("QR non détectable dans l'image : %v", err)
	}
	return r.GetText()
}
