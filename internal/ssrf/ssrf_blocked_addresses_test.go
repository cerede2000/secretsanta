package ssrf

// F8.AC1 : la récupération refuse les adresses privées, locales, de lien
// local, de métadonnées cloud et leurs équivalents IPv6, y compris via
// résolution DNS.

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"sync/atomic"
	"testing"
)

func TestForbiddenAddressClasses(t *testing.T) {
	blocked := []string{
		// boucle locale
		"127.0.0.1", "127.255.255.254", "::1",
		// non spécifiée / « ce réseau »
		"0.0.0.0", "0.1.2.3", "::",
		// privées
		"10.0.0.1", "172.16.0.1", "172.31.255.255", "192.168.1.1", "100.64.0.1",
		// lien local et métadonnées cloud
		"169.254.0.1", "169.254.169.254", "169.254.170.2", "100.100.100.200", "192.0.0.192",
		"168.63.129.16", "fd00:ec2::254", "fe80::1", "fe80::a9fe:a9fe",
		// locales uniques, site local, multidiffusion, diffusion, réservées
		"fc00::1", "fd12:3456::1", "fec0::1", "ff02::1", "224.0.0.1", "239.255.255.250",
		"255.255.255.255", "240.0.0.1", "198.18.0.1",
		// équivalents IPv6 d'adresses IPv4 internes
		"::ffff:127.0.0.1", "::ffff:10.0.0.1", "::ffff:169.254.169.254", "::ffff:192.168.0.1",
		"::127.0.0.1", "64:ff9b::7f00:1", "64:ff9b::a9fe:a9fe", "2002:7f00:1::1", "2002:a9fe:a9fe::1",
		"2001:0:4136:e378:8000:63bf:3fff:fdd2", "2001:db8::1", "100::1",
	}
	for _, s := range blocked {
		if !Forbidden(netip.MustParseAddr(s)) {
			t.Errorf("%s devrait être refusée", s)
		}
	}
	if !Forbidden(netip.MustParseAddr("fe80::1%en0")) {
		t.Error("une adresse avec zone devrait être refusée")
	}
	if !Forbidden(netip.Addr{}) {
		t.Error("une adresse invalide devrait être refusée")
	}
	allowed := []string{"93.184.216.34", "8.8.8.8", "1.1.1.1", "151.101.1.69", "2606:4700:4700::1111", "2a00:1450:4007:80b::200e"}
	for _, s := range allowed {
		if Forbidden(netip.MustParseAddr(s)) {
			t.Errorf("%s devrait être permise", s)
		}
	}
}

// internalServer compte les requêtes reçues : aucune ne doit l'atteindre.
func internalServer(t *testing.T) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		fmt.Fprint(w, "secret interne")
	}))
	t.Cleanup(srv.Close)
	return srv, &hits
}

func TestFetchRefusesInternalLiterals(t *testing.T) {
	srv, hits := internalServer(t)
	port := serverPort(t, srv)
	// Aucun crochet de test : la garde telle qu'en production, port du serveur permis.
	g := mustGuard(t, Options{AllowedPorts: []int{80, 443, port}, Resolver: newResolver(nil)})
	ctx := context.Background()
	for _, u := range []string{
		fmt.Sprintf("http://127.0.0.1:%d/", port),
		fmt.Sprintf("http://[::ffff:127.0.0.1]:%d/", port),
		fmt.Sprintf("http://[::1]:%d/", port),
		"http://169.254.169.254/latest/meta-data/",
		"http://[fd00:ec2::254]/latest/meta-data/",
		"http://100.100.100.200/latest/meta-data/",
		"http://10.0.0.1/",
		"http://[fe80::1]/",
		"http://0.0.0.0/",
	} {
		_, err := g.FetchPage(ctx, u)
		wantErr(t, err, ErrForbiddenAddress, u)
		_, err = g.FetchImage(ctx, u)
		wantErr(t, err, ErrForbiddenAddress, "image "+u)
	}
	if hits.Load() != 0 {
		t.Fatalf("le serveur interne a reçu %d requêtes", hits.Load())
	}
}

func TestFetchRefusesInternalViaDNS(t *testing.T) {
	srv, hits := internalServer(t)
	port := serverPort(t, srv)
	res := newResolver(map[string][]string{
		"localhost":         {"127.0.0.1"},
		"intranet.example":  {"127.0.0.1"},
		"metadata.example":  {"169.254.169.254"},
		"private.example":   {"192.168.1.10"},
		"v6loop.example":    {"::1"},
		"mapped.example":    {"::ffff:127.0.0.1"},
		"ula.example":       {"fd00:ec2::254"},
		"nat64.example":     {"64:ff9b::a9fe:a9fe"},
		"mixed.example":     {"93.184.216.34", "127.0.0.1"},
		"mixed6.example":    {"2606:4700:4700::1111", "fe80::1"},
		"metadata.internal": {"169.254.169.254"},
	})
	g := mustGuard(t, Options{AllowedPorts: []int{80, 443, port}, Resolver: res})
	ctx := context.Background()
	for host := range res.names {
		u := fmt.Sprintf("http://%s:%d/latest/meta-data/", host, port)
		_, err := g.FetchPage(ctx, u)
		wantErr(t, err, ErrForbiddenAddress, u)
		if res.count(host) == 0 {
			t.Fatalf("%s : le nom n'a pas été résolu par la garde", host)
		}
	}
	if hits.Load() != 0 {
		t.Fatalf("le serveur interne a reçu %d requêtes", hits.Load())
	}
}

func TestFetchRefusesSchemesPortsAndCredentials(t *testing.T) {
	res := newResolver(map[string][]string{"example.com": {"93.184.216.34"}})
	g := mustGuard(t, Options{Resolver: res})
	ctx := context.Background()
	cases := []struct {
		url  string
		want error
	}{
		{"file:///etc/passwd", ErrScheme},
		{"gopher://example.com/", ErrScheme},
		{"ftp://example.com/", ErrScheme},
		{"data:image/png;base64,AAAA", ErrScheme},
		{"javascript:alert(1)", ErrScheme},
		{"//example.com/", ErrScheme},
		{"http://example.com:22/", ErrPort},
		{"http://example.com:6379/", ErrPort},
		{"https://example.com:8443/", ErrPort},
		{"http://user:pass@example.com/", ErrURL},
		{"http:example.com", ErrURL},
	}
	for _, c := range cases {
		_, err := g.FetchPage(ctx, c.url)
		wantErr(t, err, c.want, c.url)
	}
	if res.count("example.com") != 0 {
		t.Fatal("une URL refusée ne doit même pas être résolue")
	}
}

// Seule l'URL SearXNG configurée échappe au refus des adresses internes, et
// seulement elle (même schéma, hôte, port et préfixe de chemin).
func TestSearxngExceptionIsTheOnlyOne(t *testing.T) {
	var paths []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path)
		fmt.Fprint(w, `{"results":[]}`)
	}))
	defer srv.Close()
	port := serverPort(t, srv)
	res := newResolver(map[string][]string{"searxng": {"127.0.0.1"}})
	g := mustGuard(t, Options{Resolver: res, SearxngURL: fmt.Sprintf("http://searxng:%d/searx/", port)})
	ctx := context.Background()

	ok := fmt.Sprintf("http://searxng:%d/searx/search?q=train", port)
	if _, err := g.FetchPage(ctx, ok); err != nil {
		t.Fatalf("l'URL SearXNG configurée doit passer : %v", err)
	}
	for _, u := range []string{
		fmt.Sprintf("http://searxng:%d/admin", port),
		fmt.Sprintf("http://searxng:%d/searxother", port),
		fmt.Sprintf("http://searxng:%d/searx/../admin", port),
		fmt.Sprintf("http://searxng:%d/searx/%%2e%%2e/admin", port),
		fmt.Sprintf("https://searxng:%d/searx/search", port),
		fmt.Sprintf("http://127.0.0.1:%d/searx/search", port),
		"http://searxng/searx/search",
	} {
		_, err := g.FetchPage(ctx, u)
		wantErr(t, err, nil, u)
	}
	if len(paths) != 1 || !strings.HasPrefix(paths[0], "/searx/") {
		t.Fatalf("seule la requête SearXNG devait passer : %v", paths)
	}

	// Sans exception configurée, la même URL est refusée.
	g2 := mustGuard(t, Options{Resolver: res, AllowedPorts: []int{port}})
	_, err := g2.FetchPage(ctx, ok)
	wantErr(t, err, ErrForbiddenAddress, "sans exception")
}
