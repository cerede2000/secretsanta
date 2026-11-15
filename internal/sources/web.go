package sources

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"

	"secretsanta/internal/config"
)

// maxBody plafonne les réponses de recherche.
const maxBody = 1 << 20

// Les messages d'erreur ne citent jamais l'URL ni les en-têtes (clés).
func do(client *http.Client, name string, req *http.Request, into any) error {
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("%s : requête échouée", name)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%s : statut %d", name, resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBody))
	if err != nil {
		return fmt.Errorf("%s : lecture échouée", name)
	}
	if err := json.Unmarshal(body, into); err != nil {
		return fmt.Errorf("%s : réponse illisible", name)
	}
	return nil
}

// newRequest construit une requête (sans http.NewRequest, réservé à outbound).
func newRequest(ctx context.Context, method, raw string, body []byte) (*http.Request, error) {
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return nil, errors.New("adresse de source invalide")
	}
	r := &http.Request{Method: method, URL: u, Host: u.Host, Header: http.Header{}, Proto: "HTTP/1.1", ProtoMajor: 1, ProtoMinor: 1}
	if body != nil {
		r.Body = io.NopCloser(bytes.NewReader(body))
		r.ContentLength = int64(len(body))
		r.GetBody = func() (io.ReadCloser, error) { return io.NopCloser(bytes.NewReader(body)), nil }
	}
	return r.WithContext(ctx), nil
}

func trunc(s string, n int) string {
	r := []rune(strings.TrimSpace(s))
	if len(r) > n {
		return string(r[:n])
	}
	return string(r)
}

func httpURL(s string) bool {
	return strings.HasPrefix(s, "https://") || strings.HasPrefix(s, "http://")
}

// SearXNG interroge une instance auto-hébergée.
type SearXNG struct {
	Base   string
	Client *http.Client
}

// Name identifie la source.
func (s *SearXNG) Name() string { return "searxng" }

// Search lance la recherche JSON.
func (s *SearXNG) Search(ctx context.Context, q Query) ([]Suggestion, error) {
	v := url.Values{"q": {q.text()}, "format": {"json"}, "language": {q.lang()}}
	req, err := newRequest(ctx, http.MethodGet, strings.TrimRight(s.Base, "/")+"/search?"+v.Encode(), nil)
	if err != nil {
		return nil, fmt.Errorf("searxng : %w", err)
	}
	req.Header.Set("Accept", "application/json")
	var out struct {
		Results []struct{ Title, URL, Content string } `json:"results"`
	}
	if err := do(s.Client, s.Name(), req, &out); err != nil {
		return nil, err
	}
	var res []Suggestion
	for _, r := range out.Results {
		if r.Title != "" && httpURL(r.URL) {
			res = append(res, Suggestion{Title: trunc(r.Title, 150), URL: r.URL, Description: trunc(r.Content, 300), Source: s.Name()})
		}
	}
	return res, nil
}

// Brave utilise l'API Brave Search.
type Brave struct {
	Key    config.Secret
	Base   string // défaut : API publique
	Client *http.Client
}

// Name identifie la source.
func (b *Brave) Name() string { return "brave" }

// Search lance la recherche.
func (b *Brave) Search(ctx context.Context, q Query) ([]Suggestion, error) {
	base := b.Base
	if base == "" {
		base = "https://api.search.brave.com"
	}
	v := url.Values{"q": {q.text()}, "count": {"10"}}
	req, err := newRequest(ctx, http.MethodGet, strings.TrimRight(base, "/")+"/res/v1/web/search?"+v.Encode(), nil)
	if err != nil {
		return nil, fmt.Errorf("brave : %w", err)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("X-Subscription-Token", b.Key.Reveal())
	var out struct {
		Web struct {
			Results []struct{ Title, URL, Description string } `json:"results"`
		} `json:"web"`
	}
	if err := do(b.Client, b.Name(), req, &out); err != nil {
		return nil, err
	}
	var res []Suggestion
	for _, r := range out.Web.Results {
		if r.Title != "" && httpURL(r.URL) {
			res = append(res, Suggestion{Title: trunc(r.Title, 150), URL: r.URL, Description: trunc(r.Description, 300), Source: b.Name()})
		}
	}
	return res, nil
}

// Serper utilise l'API Serper.
type Serper struct {
	Key    config.Secret
	Base   string // défaut : API publique
	Client *http.Client
}

// Name identifie la source.
func (s *Serper) Name() string { return "serper" }

// Search lance la recherche.
func (s *Serper) Search(ctx context.Context, q Query) ([]Suggestion, error) {
	base := s.Base
	if base == "" {
		base = "https://google.serper.dev"
	}
	gl := "fr"
	if q.lang() == "en" {
		gl = "us"
	}
	body, _ := json.Marshal(map[string]any{"q": q.text(), "hl": q.lang(), "gl": gl, "num": 10})
	req, err := newRequest(ctx, http.MethodPost, strings.TrimRight(base, "/")+"/search", body)
	if err != nil {
		return nil, fmt.Errorf("serper : %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-API-KEY", s.Key.Reveal())
	var out struct {
		Organic []struct{ Title, Link, Snippet string } `json:"organic"`
	}
	if err := do(s.Client, s.Name(), req, &out); err != nil {
		return nil, err
	}
	var res []Suggestion
	for _, r := range out.Organic {
		if r.Title != "" && httpURL(r.Link) {
			res = append(res, Suggestion{Title: trunc(r.Title, 150), URL: r.Link, Description: trunc(r.Snippet, 300), Source: s.Name()})
		}
	}
	return res, nil
}

// FromConfig construit la chaîne depuis la configuration : SearXNG, Brave,
// Serper (selon ce qui est défini), puis le catalogue. Le client doit venir
// de ssrf.Guard.Client() ou d'outbound.New.
func FromConfig(cfg config.Sources, client *http.Client, log *slog.Logger) *Chain {
	var list []Source
	if cfg.SearxngURL != "" {
		list = append(list, &SearXNG{Base: cfg.SearxngURL, Client: client})
	}
	if cfg.BraveKey.IsSet() {
		list = append(list, &Brave{Key: cfg.BraveKey, Client: client})
	}
	if cfg.SerperKey.IsSet() {
		list = append(list, &Serper{Key: cfg.SerperKey, Client: client})
	}
	return NewChain(log, list...)
}
