package maildiag

import (
	"context"
	"errors"
	"net"
	"strings"
)

// ErrNotFound indique qu'un nom n'a pas d'enregistrement du type demandé.
var ErrNotFound = errors.New("maildiag: enregistrement absent")

// Resolver est le résolveur DNS injectable.
type Resolver interface {
	LookupTXT(ctx context.Context, name string) ([]string, error)
	LookupMX(ctx context.Context, name string) ([]*net.MX, error)
}

// NetResolver s'appuie sur le résolveur du système.
type NetResolver struct{}

// LookupTXT implémente Resolver.
func (NetResolver) LookupTXT(ctx context.Context, name string) ([]string, error) {
	return net.DefaultResolver.LookupTXT(ctx, name)
}

// LookupMX implémente Resolver.
func (NetResolver) LookupMX(ctx context.Context, name string) ([]*net.MX, error) {
	return net.DefaultResolver.LookupMX(ctx, name)
}

func isNotFound(err error) bool {
	if errors.Is(err, ErrNotFound) {
		return true
	}
	var de *net.DNSError
	return errors.As(err, &de) && de.IsNotFound
}

// lookupTXT renvoie les enregistrements TXT ; absent donne (nil, nil).
func (d *Diagnostic) lookupTXT(ctx context.Context, name string) ([]string, error) {
	recs, err := d.res.LookupTXT(ctx, name)
	if err != nil {
		if isNotFound(err) {
			return nil, nil
		}
		return nil, err
	}
	return recs, nil
}

// withPrefix filtre les enregistrements commençant (sans casse) par prefix.
func withPrefix(recs []string, prefix string) []string {
	var out []string
	for _, r := range recs {
		if strings.HasPrefix(strings.ToLower(strings.TrimSpace(r)), prefix) {
			out = append(out, strings.TrimSpace(r))
		}
	}
	return out
}

// tags découpe un enregistrement « k=v; k=v » (clés en minuscules).
func tags(rec string) map[string]string {
	m := map[string]string{}
	for _, p := range strings.Split(rec, ";") {
		k, v, ok := strings.Cut(strings.TrimSpace(p), "=")
		if !ok {
			continue
		}
		k = strings.ToLower(strings.TrimSpace(k))
		if _, dup := m[k]; !dup {
			m[k] = strings.TrimSpace(v)
		}
	}
	return m
}
