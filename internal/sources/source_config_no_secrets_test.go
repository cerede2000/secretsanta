package sources

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"secretsanta/internal/config"
)

func TestSourcesReadFromEnvAndNoSecretLeaks(t *testing.T) {
	const brave, serper = "BRAVE-SECRET-123", "SERPER-SECRET-456"
	env := baseEnv()
	env[config.VarSearxngURL], env[config.VarBraveAPIKey], env[config.VarSerperAPIKey] = "http://127.0.0.1:1", brave, serper
	cfg, err := config.Load(func(k string) string { return env[k] })
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Sources.SearxngURL == "" || cfg.Sources.BraveKey.Reveal() != brave || cfg.Sources.SerperKey.Reveal() != serper {
		t.Fatal("sources non lues de l'environnement")
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Subscription-Token") != brave && r.Header.Get("X-API-KEY") != serper {
			t.Errorf("clé absente de l'en-tête")
		}
		if strings.Contains(r.URL.String(), "SECRET") {
			t.Errorf("clé dans l'URL")
		}
		w.WriteHeader(500)
	}))
	defer srv.Close()

	var logs bytes.Buffer
	log := slog.New(slog.NewTextHandler(&logs, nil))
	c := NewChain(log,
		&Brave{Key: cfg.Sources.BraveKey, Base: srv.URL, Client: srv.Client()},
		&Serper{Key: cfg.Sources.SerperKey, Base: srv.URL, Client: srv.Client()},
	)
	res, err2 := c.Search(context.Background(), Query{Keywords: []string{"livre"}})
	if err2 != nil {
		t.Fatal(err2)
	}
	// Erreurs directes des sources
	var all strings.Builder
	all.WriteString(logs.String())
	all.WriteString(fmt.Sprintf("%v %+v %#v", res, cfg.Sources, cfg.Sources))
	for _, s := range []Source{
		&Brave{Key: cfg.Sources.BraveKey, Base: srv.URL, Client: srv.Client()},
		&Serper{Key: cfg.Sources.SerperKey, Base: "http://127.0.0.1:1", Client: srv.Client()},
	} {
		_, e := s.Search(context.Background(), Query{})
		all.WriteString(fmt.Sprint(e))
	}
	if strings.Contains(all.String(), "SECRET") {
		t.Fatalf("clé exposée : %s", all.String())
	}
}
