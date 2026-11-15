package ssrf

// F8.AC4 : une image n'est copiée que si son contenu est réellement une image
// d'un type autorisé, et l'interface ne référence jamais d'image externe.

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

type served struct {
	ctype string
	body  []byte
}

func imageServer(t *testing.T, files map[string]served) (*Guard, string) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f, ok := files[r.URL.Path]
		if !ok {
			http.NotFound(w, r)
			return
		}
		if f.ctype != "" {
			w.Header().Set("Content-Type", f.ctype)
		} else {
			w.Header()["Content-Type"] = nil
		}
		w.Write(f.body)
	}))
	t.Cleanup(srv.Close)
	g, base := limitsGuard(t, srv, Options{})
	return g, base
}

func dirEntries(t *testing.T, dir string) []string {
	t.Helper()
	es, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range es {
		names = append(names, e.Name())
	}
	return names
}

func TestOnlyRealImagesAreCopied(t *testing.T) {
	pngData := pngOf(t, 32, 24)
	html := []byte("<!doctype html><script>alert(1)</script>")
	svg := []byte(`<svg xmlns="http://www.w3.org/2000/svg" onload="alert(1)"><rect width="10" height="10"/></svg>`)
	truncated := append([]byte{}, pngData[:40]...)
	webp := append([]byte("RIFF\x24\x00\x00\x00WEBPVP8 "), bytes.Repeat([]byte{0}, 28)...)
	polyglot := append([]byte("\x89PNG\r\n\x1a\n"), html...)
	files := map[string]served{
		"/ok.png":         {"image/png", pngData},
		"/ok.jpg":         {"image/jpeg", jpegOf(t, 16, 16)},
		"/ok.gif":         {"image/gif", gifOf(t, 8, 8)},
		"/no-ctype.png":   {"", pngData},
		"/html-as-png":    {"image/png", html},
		"/svg":            {"image/svg+xml", svg},
		"/svg-as-png":     {"image/png", svg},
		"/truncated.png":  {"image/png", truncated},
		"/webp":           {"image/webp", webp},
		"/polyglot.png":   {"image/png", polyglot},
		"/png-as-html":    {"text/html", pngData},
		"/too-wide.png":   {"image/png", pngOf(t, MaxImageDimension+1, 1)},
		"/too-many-px":    {"image/png", pngOf(t, 4000, 3001)},
		"/empty":          {"image/png", nil},
		"/text-as-binary": {"application/octet-stream", []byte("bonjour")},
	}
	g, base := imageServer(t, files)
	dir := t.TempDir()
	st, err := NewStore(dir, "/images")
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()

	for _, p := range []string{"/html-as-png", "/svg", "/svg-as-png", "/truncated.png", "/webp", "/polyglot.png", "/png-as-html", "/too-wide.png", "/too-many-px", "/empty", "/text-as-binary"} {
		_, err := st.Copy(ctx, g, base+p)
		wantErr(t, err, ErrNotImage, p)
	}
	if n := dirEntries(t, dir); len(n) != 0 {
		t.Fatalf("un contenu refusé a laissé des fichiers : %v", n)
	}

	wantTypes := map[string]string{"/ok.png": "image/png", "/ok.jpg": "image/jpeg", "/ok.gif": "image/gif", "/no-ctype.png": "image/png"}
	for p, ct := range wantTypes {
		li, err := st.Copy(ctx, g, base+p)
		if err != nil {
			t.Fatalf("%s : %v", p, err)
		}
		if li.ContentType != ct {
			t.Fatalf("%s : type %q, attendu %q", p, li.ContentType, ct)
		}
		sum := sha256.Sum256(files[p].body)
		if !strings.HasPrefix(li.Name, hex.EncodeToString(sum[:])) {
			t.Fatalf("%s : nom %q non dérivé du contenu", p, li.Name)
		}
		got, err := os.ReadFile(filepath.Join(dir, li.Name))
		if err != nil || !bytes.Equal(got, files[p].body) {
			t.Fatalf("%s : copie locale absente ou différente", p)
		}
		assertLocalOnly(t, st, li, base)
	}
	if li, _ := st.Copy(ctx, g, base+"/ok.png"); li.Width != 32 || li.Height != 24 {
		t.Fatalf("dimensions %dx%d", li.Width, li.Height)
	}
	// ok.png et no-ctype.png ont le même contenu : un seul fichier, aucun reste temporaire.
	if n := dirEntries(t, dir); len(n) != 3 {
		t.Fatalf("fichiers attendus : 3, obtenus %v", n)
	}

	// Une Image fabriquée à la main est revérifiée avant d'être écrite.
	_, err = st.Save(&Image{ContentType: "image/png", Ext: "png", Data: html})
	wantErr(t, err, ErrNotImage, "Image forgée")
}

// assertLocalOnly : ni l'adresse d'origine ni aucune adresse externe ne
// figure dans ce que l'interface reçoit ; Src est un chemin de même origine.
func assertLocalOnly(t *testing.T, st *Store, li LocalImage, origin string) {
	t.Helper()
	v := reflect.ValueOf(li)
	for i := 0; i < v.NumField(); i++ {
		if v.Field(i).Kind() != reflect.String {
			continue
		}
		s := v.Field(i).String()
		if strings.Contains(s, origin) || strings.Contains(s, "://") || strings.Contains(s, "site.example") || strings.HasPrefix(s, "//") || strings.HasPrefix(s, "data:") {
			t.Fatalf("champ %s référence une adresse externe : %q", v.Type().Field(i).Name, s)
		}
	}
	if !strings.HasPrefix(li.Src, "/images/") || !st.IsLocalSrc(li.Src) {
		t.Fatalf("Src %q n'est pas un chemin local", li.Src)
	}
	// Image ne garde pas non plus d'adresse d'origine.
	it := reflect.TypeOf(Image{})
	for i := 0; i < it.NumField(); i++ {
		if n := strings.ToLower(it.Field(i).Name); strings.Contains(n, "url") || strings.Contains(n, "src") || strings.Contains(n, "origin") {
			t.Fatalf("Image garde une adresse : champ %s", it.Field(i).Name)
		}
	}
}

func TestIsLocalSrcRefusesExternalReferences(t *testing.T) {
	st, err := NewStore(t.TempDir(), "/images")
	if err != nil {
		t.Fatal(err)
	}
	h := strings.Repeat("a", 64)
	for _, s := range []string{
		"https://m.media-amazon.com/images/I/x.jpg", "//evil.example/" + h + ".png", "http://localhost/images/" + h + ".png",
		"data:image/png;base64,AAAA", "/images/../" + h + ".png", "/images/" + h + ".svg", "/images/x.png", "",
		"/imagesX/" + h + ".png", "/images/" + h + ".png?x=https://evil.example",
	} {
		if st.IsLocalSrc(s) {
			t.Errorf("%q accepté comme source locale", s)
		}
	}
	if !st.IsLocalSrc("/images/" + h + ".png") {
		t.Error("source locale valide refusée")
	}
	for _, bad := range []string{"https://cdn.example/images", "//cdn.example", "images", "data:", "/a:b"} {
		if _, err := NewStore(t.TempDir(), bad); err == nil {
			t.Errorf("préfixe %q accepté", bad)
		}
	}
}

func TestLocalImageHandlerServesOnlyLocalCopies(t *testing.T) {
	dir := t.TempDir()
	st, err := NewStore(dir, "/images")
	if err != nil {
		t.Fatal(err)
	}
	img, err := CheckImage(pngOf(t, 4, 4))
	if err != nil {
		t.Fatal(err)
	}
	li, err := st.Save(img)
	if err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(dir, "secret.txt"), []byte("non"), 0o600)
	h := http.StripPrefix("/images", st.Handler())

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, li.Src, nil))
	if rec.Code != 200 || rec.Header().Get("Content-Type") != "image/png" || rec.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Fatalf("service de la copie : %d %v", rec.Code, rec.Header())
	}
	if !bytes.Equal(rec.Body.Bytes(), img.Data) {
		t.Fatal("contenu servi différent")
	}
	for _, p := range []string{"/images/secret.txt", "/images/../go.mod", "/images/" + strings.Repeat("b", 64) + ".png", "/images/" + strings.TrimSuffix(li.Name, ".png") + ".gif"} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, p, nil))
		if rec.Code != http.StatusNotFound {
			t.Fatalf("%s : statut %d, attendu 404", p, rec.Code)
		}
	}
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, li.Src, nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("POST : %d", rec.Code)
	}
}
