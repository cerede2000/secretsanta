package ssrf

// F8.AC2 : la récupération résiste au DNS rebinding : l'adresse connectée est
// revalidée et une redirection vers une adresse interdite est refusée.

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"sync/atomic"
	"testing"
	"time"
)

// Le contrôle du dialer refuse toute adresse connectée interdite.
func TestDialerControlRefusesConnectedAddress(t *testing.T) {
	g := mustGuard(t, Options{})
	for _, a := range []string{"127.0.0.1:80", "[::1]:443", "169.254.169.254:80", "[::ffff:10.0.0.1]:80", "[fd00:ec2::254]:80", "[fe80::1%en0]:80", "pas-une-adresse"} {
		if err := g.control("tcp", a, nil); err == nil {
			t.Errorf("%s : connexion permise", a)
		}
	}
	for _, a := range []string{"93.184.216.34:443", "[2606:4700:4700::1111]:80"} {
		if err := g.control("tcp", a, nil); err != nil {
			t.Errorf("%s : refus inattendu %v", a, err)
		}
	}
}

// Même si la vérification de la résolution était contournée, le connect(2)
// vers une adresse interdite n'a jamais lieu : le contrôle est dans le dialer.
func TestConnectToForbiddenAddressNeverHappens(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	var accepted atomic.Int32
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			accepted.Add(1)
			c.Close()
		}
	}()
	g := mustGuard(t, Options{})
	_, port, _ := net.SplitHostPort(ln.Addr().String())
	_, err = g.dialIPs(context.Background(), "tcp", []netip.Addr{netip.MustParseAddr("127.0.0.1")}, port, g.control)
	wantErr(t, err, ErrForbiddenAddress, "connexion directe à la boucle locale")
	time.Sleep(50 * time.Millisecond)
	if accepted.Load() != 0 {
		t.Fatal("la connexion a atteint le serveur interne")
	}
}

// Rebinding : le nom répond d'abord une adresse « publique » puis une adresse
// interne. L'adresse validée est celle qui est connectée (une seule
// résolution par connexion) et la réponse interne suivante est refusée.
func TestDNSRebindingIsRefused(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		fmt.Fprint(w, "ok")
	}))
	defer srv.Close()
	port := serverPort(t, srv)
	res := newResolver(nil)
	res.next = func(host string, call int) []netip.Addr {
		if host != "rebind.example" {
			return nil
		}
		if call == 1 {
			return []netip.Addr{netip.MustParseAddr("127.0.0.1")} // « publique » par le crochet de test
		}
		return []netip.Addr{netip.MustParseAddr("169.254.169.254")}
	}
	g := mustGuard(t, Options{Resolver: res, AllowedPorts: []int{port}})
	g.allowAddr = loopbackAsPublic
	ctx := context.Background()
	u := fmt.Sprintf("http://rebind.example:%d/", port)

	if _, err := g.FetchPage(ctx, u); err != nil {
		t.Fatalf("premier accès : %v", err)
	}
	if n := res.count("rebind.example"); n != 1 {
		t.Fatalf("une seule résolution par connexion attendue, %d", n)
	}
	_, err := g.FetchPage(ctx, u)
	wantErr(t, err, ErrForbiddenAddress, "seconde résolution interne")
	if hits.Load() != 1 {
		t.Fatalf("requêtes reçues : %d", hits.Load())
	}

	// Rebinding vers un mélange public + interne : l'hôte entier est refusé.
	res.next = func(host string, _ int) []netip.Addr {
		return []netip.Addr{netip.MustParseAddr("127.0.0.1"), netip.MustParseAddr("10.0.0.5")}
	}
	_, err = g.FetchPage(ctx, u)
	wantErr(t, err, ErrForbiddenAddress, "réponse mixte")
}

func TestRedirectToForbiddenTargetIsRefused(t *testing.T) {
	// Serveur interne sur ::1 (refusé même avec le crochet, qui ne vise que 127.0.0.1).
	var internalHits atomic.Int32
	internal := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		internalHits.Add(1)
		fmt.Fprint(w, "secret")
	}))
	ln6, err := net.Listen("tcp", "[::1]:0")
	if err == nil {
		internal.Listener = ln6
		internal.Start()
		defer internal.Close()
	}

	var target string
	ext := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target, http.StatusFound)
	}))
	defer ext.Close()
	port := serverPort(t, ext)
	ports := []int{80, 443, port}
	if internal.Listener != nil {
		ports = append(ports, serverPort(t, internal))
	}
	res := newResolver(map[string][]string{
		"public.example":   {"127.0.0.1"},
		"metadata.example": {"169.254.169.254"},
		"intranet.example": {"10.1.2.3"},
		"v6.example":       {"::1"},
	})
	g := mustGuard(t, Options{Resolver: res, AllowedPorts: ports})
	g.allowAddr = loopbackAsPublic
	ctx := context.Background()
	start := fmt.Sprintf("http://public.example:%d/go", port)

	cases := []struct {
		target string
		want   error
	}{
		{"http://169.254.169.254/latest/meta-data/", ErrForbiddenAddress},
		{"http://metadata.example/latest/meta-data/", ErrForbiddenAddress},
		{"http://intranet.example/", ErrForbiddenAddress},
		{"http://[fd00:ec2::254]/", ErrForbiddenAddress},
		{"http://[::ffff:169.254.169.254]/", ErrForbiddenAddress},
		{"file:///etc/passwd", nil},
		{"gopher://public.example/", nil},
		{"http://public.example:25/", ErrPort},
		{"http://user:pw@public.example/", ErrURL},
	}
	if internal.Listener != nil {
		ip := serverPort(t, internal)
		cases = append(cases,
			struct {
				target string
				want   error
			}{fmt.Sprintf("http://[::1]:%d/", ip), ErrForbiddenAddress},
			struct {
				target string
				want   error
			}{fmt.Sprintf("http://v6.example:%d/", ip), ErrForbiddenAddress})
	}
	for _, c := range cases {
		target = c.target
		_, err := g.FetchPage(ctx, start)
		wantErr(t, err, c.want, "redirection vers "+c.target)
		_, err = g.FetchImage(ctx, start)
		wantErr(t, err, c.want, "image redirigée vers "+c.target)
	}
	if internalHits.Load() != 0 {
		t.Fatalf("le serveur interne a reçu %d requêtes", internalHits.Load())
	}

	// Une redirection vers une cible permise est suivie.
	ok := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { fmt.Fprint(w, "fin") }))
	defer ok.Close()
	g2 := mustGuard(t, Options{Resolver: res, AllowedPorts: append(ports, serverPort(t, ok))})
	g2.allowAddr = loopbackAsPublic
	target = fmt.Sprintf("http://public.example:%d/fin", serverPort(t, ok))
	r, err := g2.FetchPage(ctx, start)
	if err != nil || string(r.Body) != "fin" || r.URL.Path != "/fin" {
		t.Fatalf("redirection permise : %v %+v", err, r)
	}
}
