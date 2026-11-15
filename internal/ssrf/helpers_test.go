package ssrf

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/color"
	"image/gif"
	"image/jpeg"
	"image/png"
	"net"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"strconv"
	"sync"
	"testing"
)

// fakeResolver remplace le DNS : chaque nom renvoie les adresses données ;
// next, s'il est fourni, choisit la réponse selon le numéro d'appel.
type fakeResolver struct {
	mu    sync.Mutex
	names map[string][]netip.Addr
	next  func(host string, call int) []netip.Addr
	calls map[string]int
}

func newResolver(pairs map[string][]string) *fakeResolver {
	r := &fakeResolver{names: map[string][]netip.Addr{}, calls: map[string]int{}}
	for host, addrs := range pairs {
		for _, a := range addrs {
			r.names[host] = append(r.names[host], netip.MustParseAddr(a))
		}
	}
	return r
}

func (r *fakeResolver) LookupNetIP(_ context.Context, _ string, host string) ([]netip.Addr, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls[host]++
	if r.next != nil {
		if a := r.next(host, r.calls[host]); a != nil {
			return a, nil
		}
	}
	a, ok := r.names[host]
	if !ok {
		return nil, &net.DNSError{Err: "no such host", Name: host, IsNotFound: true}
	}
	return a, nil
}

func (r *fakeResolver) count(host string) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.calls[host]
}

func serverPort(t *testing.T, srv *httptest.Server) int {
	t.Helper()
	u, err := url.Parse(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	p, err := strconv.Atoi(u.Port())
	if err != nil {
		t.Fatal(err)
	}
	return p
}

// loopbackAsPublic est le seul crochet de test : il fait passer 127.0.0.1
// (et seulement elle) pour une adresse publique, afin que le serveur de test
// joue le rôle d'un site externe.
func loopbackAsPublic(a netip.Addr) bool { return a == netip.MustParseAddr("127.0.0.1") }

func mustGuard(t *testing.T, opts Options) *Guard {
	t.Helper()
	g, err := New(opts)
	if err != nil {
		t.Fatal(err)
	}
	return g
}

func pngOf(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	img.Set(0, 0, color.RGBA{R: 196, G: 30, B: 58, A: 255})
	var b bytes.Buffer
	if err := png.Encode(&b, img); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func jpegOf(t *testing.T, w, h int) []byte {
	t.Helper()
	var b bytes.Buffer
	if err := jpeg.Encode(&b, image.NewRGBA(image.Rect(0, 0, w, h)), nil); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func gifOf(t *testing.T, w, h int) []byte {
	t.Helper()
	var b bytes.Buffer
	pal := image.NewPaletted(image.Rect(0, 0, w, h), color.Palette{color.Black, color.White})
	if err := gif.Encode(&b, pal, nil); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func wantErr(t *testing.T, err, target error, what string) {
	t.Helper()
	if err == nil {
		t.Fatalf("%s : refus attendu, aucune erreur", what)
	}
	if target != nil && !errors.Is(err, target) {
		t.Fatalf("%s : erreur %v, attendu %v", what, err, target)
	}
}
