package ssrf

// F8.AC3 : le nombre de redirections, la taille et le délai de récupération
// sont plafonnés.

import (
	"bytes"
	"compress/gzip"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func limitsGuard(t *testing.T, srv *httptest.Server, opts Options) (*Guard, string) {
	t.Helper()
	port := serverPort(t, srv)
	opts.Resolver = newResolver(map[string][]string{"site.example": {"127.0.0.1"}})
	opts.AllowedPorts = []int{port}
	g := mustGuard(t, opts)
	g.allowAddr = loopbackAsPublic
	return g, fmt.Sprintf("http://site.example:%d", port)
}

func TestRedirectsAreCappedAtThree(t *testing.T) {
	if MaxRedirects != 3 {
		t.Fatalf("plafond de redirections %d, attendu 3", MaxRedirects)
	}
	var finals atomic.Int32
	// /r/N redirige vers /r/N-1 ; /r/0 répond.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n, _ := strconv.Atoi(strings.TrimPrefix(r.URL.Path, "/r/"))
		if n == 0 {
			finals.Add(1)
			fmt.Fprint(w, "fin")
			return
		}
		http.Redirect(w, r, fmt.Sprintf("/r/%d", n-1), http.StatusFound)
	}))
	defer srv.Close()
	g, base := limitsGuard(t, srv, Options{})
	ctx := context.Background()

	r, err := g.FetchPage(ctx, base+"/r/3")
	if err != nil || string(r.Body) != "fin" {
		t.Fatalf("3 redirections doivent passer : %v", err)
	}
	_, err = g.FetchPage(ctx, base+"/r/4")
	wantErr(t, err, ErrTooManyRedirects, "4 redirections")
	_, err = g.FetchImage(ctx, base+"/r/50")
	wantErr(t, err, ErrTooManyRedirects, "50 redirections")
	if finals.Load() != 1 {
		t.Fatalf("la cible au-delà du plafond a été atteinte : %d", finals.Load())
	}
	// Le client exposé applique le même plafond.
	req, _ := http.NewRequest(http.MethodGet, base+"/r/4", nil)
	resp, err := g.Client().Do(req)
	if err == nil {
		resp.Body.Close()
	}
	wantErr(t, err, ErrTooManyRedirects, "client exposé")
}

func TestSizeIsCapped(t *testing.T) {
	const max = 1024
	var gz bytes.Buffer
	zw := gzip.NewWriter(&gz)
	zw.Write(bytes.Repeat([]byte("a"), 8<<20))
	zw.Close()
	bigPNG := pngOf(t, 400, 400)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/exact":
			w.Write(bytes.Repeat([]byte("a"), max))
		case "/declared":
			w.Header().Set("Content-Length", strconv.Itoa(max+1))
			w.Write(bytes.Repeat([]byte("a"), max+1))
		case "/chunked":
			// Sans Content-Length : flux découpé, coupé à la lecture.
			for i := 0; i < 64; i++ {
				w.Write(bytes.Repeat([]byte("a"), 512))
				w.(http.Flusher).Flush()
			}
		case "/gzip-bomb":
			w.Header().Set("Content-Encoding", "gzip")
			w.Write(gz.Bytes())
		case "/big-image":
			w.Header().Set("Content-Type", "image/png")
			w.Write(bigPNG)
		}
	}))
	defer srv.Close()
	g, base := limitsGuard(t, srv, Options{MaxPageBytes: max, MaxImageBytes: max})
	ctx := context.Background()

	if r, err := g.FetchPage(ctx, base+"/exact"); err != nil || len(r.Body) != max {
		t.Fatalf("taille exacte : %v", err)
	}
	for _, p := range []string{"/declared", "/chunked", "/gzip-bomb"} {
		_, err := g.FetchPage(ctx, base+p)
		wantErr(t, err, ErrTooLarge, p)
	}
	_, err := g.FetchImage(ctx, base+"/big-image")
	wantErr(t, err, ErrTooLarge, "image trop lourde")

	// Les réglages ne dépassent jamais les plafonds.
	g2 := mustGuard(t, Options{MaxPageBytes: 1 << 40, MaxImageBytes: 1 << 40})
	if g2.maxPageBytes != MaxBytesCap || g2.maxImageBytes != MaxBytesCap {
		t.Fatalf("plafond de taille non appliqué : %d %d", g2.maxPageBytes, g2.maxImageBytes)
	}
	g3 := mustGuard(t, Options{})
	if g3.maxPageBytes != DefaultMaxPageBytes || g3.maxImageBytes != DefaultMaxImageBytes {
		t.Fatal("tailles par défaut")
	}
}

func TestTimeIsCapped(t *testing.T) {
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/slow-headers":
			select {
			case <-release:
			case <-r.Context().Done():
			}
		case "/slow-body":
			w.Header().Set("Content-Type", "text/html")
			for {
				if _, err := w.Write([]byte("a")); err != nil {
					return
				}
				w.(http.Flusher).Flush()
				select {
				case <-release:
					return
				case <-r.Context().Done():
					return
				case <-time.After(50 * time.Millisecond):
				}
			}
		}
	}))
	defer srv.Close()
	defer close(release)
	const timeout = 300 * time.Millisecond
	g, base := limitsGuard(t, srv, Options{Timeout: timeout})
	ctx := context.Background()
	for _, p := range []string{"/slow-headers", "/slow-body"} {
		start := time.Now()
		_, err := g.FetchPage(ctx, base+p)
		wantErr(t, err, nil, p)
		if d := time.Since(start); d > 3*time.Second {
			t.Fatalf("%s : délai non plafonné (%v)", p, d)
		}
	}

	if g := mustGuard(t, Options{Timeout: time.Hour}); g.timeout != MaxTimeout || g.client.Timeout != MaxTimeout {
		t.Fatalf("plafond de délai non appliqué : %v %v", g.timeout, g.client.Timeout)
	}
	if g := mustGuard(t, Options{}); g.timeout != DefaultTimeout || g.client.Timeout != DefaultTimeout {
		t.Fatal("délai par défaut")
	}
}
