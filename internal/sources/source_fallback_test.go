package sources

import (
	"context"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"secretsanta/internal/config"
)

func TestFallbackOrderAndFailingSourceDoesNotBlock(t *testing.T) {
	var calls []string
	mk := func(name string, code int, body string) *httptest.Server {
		return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			calls = append(calls, name)
			w.WriteHeader(code)
			_, _ = w.Write([]byte(body))
		}))
	}
	sx := mk("searxng", 500, "")
	defer sx.Close()
	br := mk("brave", 200, `{"web":{"results":[]}}`)
	defer br.Close()
	se := mk("serper", 200, `{"organic":[{"title":"Plaid","link":"https://exemple.fr/p","snippet":"doux"}]}`)
	defer se.Close()

	c := NewChain(nil,
		&SearXNG{Base: sx.URL, Client: sx.Client()},
		&Brave{Key: config.NewSecret("k1"), Base: br.URL, Client: br.Client()},
		&Serper{Key: config.NewSecret("k2"), Base: se.URL, Client: se.Client()},
	)
	if want := []string{"searxng", "brave", "serper", "catalogue"}; !reflect.DeepEqual(c.Names(), want) {
		t.Fatalf("ordre %v", c.Names())
	}
	res, err := c.Search(context.Background(), Query{Keywords: []string{"plaid"}, Band: Band20to50})
	if err != nil || len(res) != 1 || res[0].Source != "serper" {
		t.Fatalf("res=%v err=%v", res, err)
	}
	if !reflect.DeepEqual(calls, []string{"searxng", "brave", "serper"}) {
		t.Fatalf("appels %v", calls)
	}
}

func TestSearxngFirstStopsChain(t *testing.T) {
	hit := false
	sx := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"results":[{"title":"Thé","url":"https://exemple.fr/t","content":"x"}]}`))
	}))
	defer sx.Close()
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { hit = true }))
	defer other.Close()
	c := NewChain(nil, &SearXNG{Base: sx.URL, Client: sx.Client()}, &Brave{Key: config.NewSecret("k"), Base: other.URL, Client: other.Client()})
	res, err := c.Search(context.Background(), Query{})
	if err != nil || res[0].Source != "searxng" || hit {
		t.Fatalf("res=%v err=%v hit=%v", res, err, hit)
	}
}

func TestAllDownFallsBackToCatalog(t *testing.T) {
	dead := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	url := dead.URL
	dead.Close()
	c := NewChain(nil, &SearXNG{Base: url, Client: http.DefaultClient})
	res, err := c.Search(context.Background(), Query{Band: BandUnder20, Lang: "en"})
	if err != nil || len(res) == 0 || res[0].Source != "catalogue" {
		t.Fatalf("res=%v err=%v", res, err)
	}
}
