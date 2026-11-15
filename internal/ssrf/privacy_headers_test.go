package ssrf

// Les récupérations de page ou d'image n'envoient ni Referer, ni cookie, ni
// identifiant, et suivent les redirections sans en garder.

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"secretsanta/internal/shared/outbound"
)

func TestFetchSendsNoIdentity(t *testing.T) {
	var mu sync.Mutex
	var seen []http.Header
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		seen = append(seen, r.Header.Clone())
		mu.Unlock()
		if r.URL.Path == "/start" {
			http.SetCookie(w, &http.Cookie{Name: "sid", Value: "1"})
			http.Redirect(w, r, "/end", http.StatusFound)
			return
		}
		w.Write([]byte("ok"))
	}))
	defer srv.Close()
	g, base := limitsGuard(t, srv, Options{})
	if g.Client().Jar != nil {
		t.Fatal("aucun magasin de cookies")
	}
	if _, err := g.FetchPage(context.Background(), base+"/start"); err != nil {
		t.Fatal(err)
	}
	req, _ := http.NewRequest(http.MethodGet, base+"/start", nil)
	req.Header.Set("Referer", "https://app.example/groupe/noel")
	req.Header.Set("Cookie", "session=abc")
	req.Header.Set("Authorization", "Bearer x")
	resp, err := g.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if len(seen) != 4 {
		t.Fatalf("requêtes : %d", len(seen))
	}
	for _, h := range seen {
		if h.Get("Referer") != "" || h.Get("Cookie") != "" || h.Get("Authorization") != "" || h.Get("User-Agent") != outbound.UserAgent {
			t.Fatalf("en-têtes identifiants envoyés : %v", h)
		}
	}
}
