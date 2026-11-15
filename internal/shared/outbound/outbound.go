// Package outbound fournit la fabrique unique de client HTTP sortant.
// Tout appel HTTP sortant de l'application doit passer par elle.
package outbound

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/url"
	"time"
)

// UserAgent est l'identifiant générique envoyé, sans version ni identité.
const UserAgent = "Mozilla/5.0 (compatible; SecretSanta)"

// Options règle la fabrique.
type Options struct {
	// Timeout par requête ; 15 s par défaut.
	Timeout time.Duration
	// Base est le transport sous-jacent (ex. garde SSRF de F8) ; défaut http.DefaultTransport.
	Base http.RoundTripper
}

type sanitizingTransport struct{ base http.RoundTripper }

func (t sanitizingTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	r2 := r.Clone(r.Context())
	r2.Header.Del("Referer")
	r2.Header.Del("Cookie")
	r2.Header.Del("Authorization")
	r2.Header.Set("User-Agent", UserAgent)
	return t.base.RoundTrip(r2)
}

// New renvoie un client sans cookies, sans Referer, au User-Agent générique.
func New(opts Options) *http.Client {
	base := opts.Base
	if base == nil {
		base = http.DefaultTransport
	}
	timeout := opts.Timeout
	if timeout <= 0 {
		timeout = 15 * time.Second
	}
	return &http.Client{
		Transport: sanitizingTransport{base: base},
		Jar:       nil,
		Timeout:   timeout,
	}
}

// NewRequest construit une requête sortante liée à ctx : c'est le seul endroit
// où l'application fabrique une http.Request (le test de ce paquet interdit
// http.NewRequest ailleurs). body peut être nil ; l'adresse u est déjà validée
// par l'appelant (garde SSRF, schéma, hôte).
func NewRequest(ctx context.Context, method string, u *url.URL, body []byte) (*http.Request, error) {
	var rd io.Reader
	if body != nil {
		rd = bytes.NewReader(body)
	}
	r, err := http.NewRequestWithContext(ctx, method, u.String(), rd)
	if err != nil {
		return nil, err
	}
	if body != nil {
		r.GetBody = func() (io.ReadCloser, error) { return io.NopCloser(bytes.NewReader(body)), nil }
	}
	return r, nil
}
