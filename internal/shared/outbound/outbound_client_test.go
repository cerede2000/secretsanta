package outbound

import (
	"io/fs"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestClientSendsGenericHeadersOnly(t *testing.T) {
	var got http.Header
	hits := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Clone()
		hits++
		if hits == 1 {
			http.SetCookie(w, &http.Cookie{Name: "s", Value: "1"})
			http.Redirect(w, r, "/next", http.StatusFound)
		}
	}))
	defer srv.Close()

	c := New(Options{})
	if c.Jar != nil {
		t.Fatal("pas de jar de cookies")
	}
	req, _ := http.NewRequest("GET", srv.URL, nil)
	req.Header.Set("Referer", "http://app/groupe/1")
	req.Header.Set("Cookie", "session=abc")
	req.Header.Set("User-Agent", "moi@example.org")
	resp, err := c.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if hits != 2 {
		t.Fatalf("redirection attendue, %d requêtes", hits)
	}
	if got.Get("User-Agent") != UserAgent {
		t.Fatalf("User-Agent %q", got.Get("User-Agent"))
	}
	if got.Get("Referer") != "" || got.Get("Cookie") != "" || got.Get("Authorization") != "" {
		t.Fatalf("en-têtes interdits: %v", got)
	}
	_ = cookiejar.Jar{}
}

// Aucun appel sortant hors de cette fabrique.
func TestNoOutboundCallsOutsideFactory(t *testing.T) {
	root := filepath.Join("..", "..")
	banned := []string{"http.Get(", "http.Post(", "http.PostForm(", "http.Head(", "http.DefaultClient", "http.Client{", "http.NewRequest(", "http.NewRequestWithContext("}
	outDir, _ := filepath.Abs(".")
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		abs, _ := filepath.Abs(p)
		if d.IsDir() && abs == outDir {
			return filepath.SkipDir
		}
		if d.IsDir() || !strings.HasSuffix(p, ".go") || strings.HasSuffix(p, "_test.go") {
			return nil
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		for _, s := range banned {
			if strings.Contains(string(b), s) {
				t.Errorf("%s: appel sortant interdit %q (utiliser outbound.New)", p, s)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
