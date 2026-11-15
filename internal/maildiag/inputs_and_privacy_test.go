package maildiag

import (
	"context"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"secretsanta/internal/shared/outbound"
)

// Les noms interrogés viennent uniquement de la configuration.
func TestOnlyConfigurationValuesAreQueried(t *testing.T) {
	r := newResolver()
	cfg := baseConfig()
	cfg.SMTP.From = "Père Noël <noel@Example.ORG>"
	cfg.Mail.EnvelopeFrom = "bounce@example.org"
	rep := New(Options{Config: cfg, Resolver: r, Client: &http.Client{Transport: failTransport{}}}).Run(context.Background())
	if rep.Domain != "example.org" {
		t.Fatalf("domaine %q", rep.Domain)
	}
	allowed := map[string]bool{"example.org": true, "_dmarc.example.org": true}
	for _, n := range r.seen {
		if !allowed[n] {
			t.Fatalf("nom interrogé hors configuration : %q", n)
		}
	}
	if len(r.seen) == 0 {
		t.Fatal("aucune requête DNS")
	}
}

// La seule requête HTTP est GET /health, sans identité ni donnée personnelle.
func TestHTTPRequestCarriesNoPersonalData(t *testing.T) {
	var mu sync.Mutex
	var reqs []*http.Request
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		reqs = append(reqs, r.Clone(context.Background()))
		mu.Unlock()
	}))
	defer srv.Close()
	cfg := baseConfig()
	cfg.PublicURL = srv.URL + "/?x=1"
	cfg.SMTP.Host = ""
	d := New(Options{Config: cfg, Resolver: newResolver()}) // client par défaut : outbound.New
	d.Run(context.Background())
	if len(reqs) != 1 {
		t.Fatalf("%d requêtes", len(reqs))
	}
	r := reqs[0]
	if r.Method != "GET" || r.URL.Path != "/health" || r.URL.RawQuery != "" {
		t.Fatalf("%s %s", r.Method, r.URL)
	}
	if r.Header.Get("User-Agent") != outbound.UserAgent {
		t.Fatalf("User-Agent %q : le client sortant n'est pas utilisé", r.Header.Get("User-Agent"))
	}
	for _, h := range []string{"Cookie", "Authorization", "Referer", "From"} {
		if r.Header.Get(h) != "" {
			t.Fatalf("en-tête %s présent", h)
		}
	}
}

// Aucun client HTTP en dehors de la fabrique sortante, et l'API ne prend
// aucun hôte en paramètre.
func TestNoDirectHTTPClientAndNoHostInput(t *testing.T) {
	banned := []string{"http.Get(", "http.Post(", "http.DefaultClient", "http.Client{", "http.NewRequest(", "http.NewRequestWithContext(", "os.Getenv("}
	_ = filepath.WalkDir(".", func(p string, de fs.DirEntry, err error) error {
		if err != nil || de.IsDir() || !strings.HasSuffix(p, ".go") || strings.HasSuffix(p, "_test.go") {
			return nil
		}
		b, _ := os.ReadFile(p)
		for _, s := range banned {
			if strings.Contains(string(b), s) {
				t.Errorf("%s contient %q", p, s)
			}
		}
		return nil
	})
}
