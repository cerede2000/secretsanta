package ssrf

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"syscall"
	"time"

	"secretsanta/internal/shared/outbound"
)

// Limites fixes de la garde.
const (
	// MaxRedirects est le nombre maximal de redirections suivies.
	MaxRedirects = 3
	// DefaultTimeout est le délai total par récupération (connexion, en-têtes et corps).
	DefaultTimeout = 10 * time.Second
	// MaxTimeout plafonne Options.Timeout.
	MaxTimeout = 30 * time.Second
	// DefaultMaxPageBytes est la taille maximale d'une page.
	DefaultMaxPageBytes int64 = 2 << 20
	// DefaultMaxImageBytes est la taille maximale d'une image.
	DefaultMaxImageBytes int64 = 5 << 20
	// MaxBytesCap plafonne les tailles réglables.
	MaxBytesCap int64 = 10 << 20
	// maxHeaderBytes plafonne les en-têtes de réponse.
	maxHeaderBytes = 64 << 10
	dialTimeout    = 5 * time.Second
	tlsTimeout     = 5 * time.Second
)

// Erreurs de la garde ; elles traversent les enveloppes (errors.Is).
var (
	ErrScheme           = errors.New("ssrf: schéma refusé (http ou https seulement)")
	ErrPort             = errors.New("ssrf: port refusé")
	ErrURL              = errors.New("ssrf: adresse invalide")
	ErrForbiddenAddress = errors.New("ssrf: adresse interne refusée")
	ErrTooManyRedirects = errors.New("ssrf: trop de redirections")
	ErrTooLarge         = errors.New("ssrf: réponse trop volumineuse")
	ErrStatus           = errors.New("ssrf: statut de réponse refusé")
	ErrUnresolvable     = errors.New("ssrf: nom introuvable")
)

// Resolver résout un nom d'hôte ; net.DefaultResolver convient.
type Resolver interface {
	LookupNetIP(ctx context.Context, network, host string) ([]netip.Addr, error)
}

// Options règle la garde. Les valeurs nulles prennent les défauts ; les
// valeurs trop grandes sont ramenées aux plafonds.
type Options struct {
	// Timeout est le délai total d'une récupération ; défaut 10 s, plafond 30 s.
	Timeout time.Duration
	// MaxPageBytes est la taille maximale d'une page ; défaut 2 Mio, plafond 10 Mio.
	MaxPageBytes int64
	// MaxImageBytes est la taille maximale d'une image ; défaut 5 Mio, plafond 10 Mio.
	MaxImageBytes int64
	// AllowedPorts liste les ports joignables ; défaut 80 et 443.
	AllowedPorts []int
	// Resolver résout les noms ; défaut net.DefaultResolver.
	Resolver Resolver
	// SearxngURL est la seule exception permise : cette URL (schéma, hôte,
	// port et préfixe de chemin identiques) peut viser une adresse interne.
	SearxngURL string
}

// Guard récupère des pages et des images en refusant toute cible interne.
// Elle est sûre pour un usage concurrent.
type Guard struct {
	timeout       time.Duration
	maxPageBytes  int64
	maxImageBytes int64
	ports         map[int]bool
	resolver      Resolver
	exception     *exception
	normal        *http.Transport
	excepted      *http.Transport
	client        *http.Client

	// allowAddr, réservé aux tests du paquet, traite certaines adresses
	// comme publiques (serveur de test sur la boucle locale).
	allowAddr func(netip.Addr) bool
}

type exception struct {
	scheme string
	host   string // nom d'hôte en minuscules, sans crochets
	port   int
	path   string // préfixe sans « / » final ("" = tout chemin)
}

// Response est une réponse récupérée et entièrement lue.
type Response struct {
	// URL est l'adresse finale après redirections.
	URL         *url.URL
	StatusCode  int
	ContentType string
	Body        []byte
}

// New construit une garde.
func New(opts Options) (*Guard, error) {
	g := &Guard{
		timeout:       clampDuration(opts.Timeout, DefaultTimeout, MaxTimeout),
		maxPageBytes:  clampBytes(opts.MaxPageBytes, DefaultMaxPageBytes),
		maxImageBytes: clampBytes(opts.MaxImageBytes, DefaultMaxImageBytes),
		ports:         map[int]bool{},
		resolver:      opts.Resolver,
	}
	if g.resolver == nil {
		g.resolver = net.DefaultResolver
	}
	ports := opts.AllowedPorts
	if len(ports) == 0 {
		ports = []int{80, 443}
	}
	for _, p := range ports {
		if p < 1 || p > 65535 {
			return nil, fmt.Errorf("%w: %d", ErrPort, p)
		}
		g.ports[p] = true
	}
	if s := strings.TrimSpace(opts.SearxngURL); s != "" {
		ex, err := parseException(s)
		if err != nil {
			return nil, err
		}
		g.exception = ex
	}
	g.normal = g.newTransport(g.dialNormal)
	g.excepted = g.newTransport(g.dialException)
	g.client = outbound.New(outbound.Options{Timeout: g.timeout, Base: roundTripper{g}})
	g.client.CheckRedirect = g.checkRedirect
	return g, nil
}

func clampDuration(v, def, max time.Duration) time.Duration {
	if v <= 0 {
		return def
	}
	if v > max {
		return max
	}
	return v
}

func clampBytes(v, def int64) int64 {
	if v <= 0 {
		return def
	}
	if v > MaxBytesCap {
		return MaxBytesCap
	}
	return v
}

func parseException(raw string) (*exception, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrURL, err)
	}
	scheme, host, port, err := target(u)
	if err != nil {
		return nil, err
	}
	p := strings.TrimRight(u.Path, "/")
	if hasDotSegment(p) {
		return nil, ErrURL
	}
	return &exception{scheme: scheme, host: host, port: port, path: p}, nil
}

// target valide la forme d'une URL et renvoie schéma, hôte et port effectif.
func target(u *url.URL) (scheme, host string, port int, err error) {
	if u == nil {
		return "", "", 0, ErrURL
	}
	scheme = strings.ToLower(u.Scheme)
	if scheme != "http" && scheme != "https" {
		return "", "", 0, ErrScheme
	}
	if u.Opaque != "" || u.User != nil || u.Host == "" {
		return "", "", 0, ErrURL
	}
	host = strings.ToLower(u.Hostname())
	if host == "" || strings.ContainsAny(host, "%\\ ") {
		return "", "", 0, ErrURL
	}
	ps := u.Port()
	switch {
	case ps == "" && scheme == "http":
		port = 80
	case ps == "" && scheme == "https":
		port = 443
	default:
		port, err = strconv.Atoi(ps)
		if err != nil || port < 1 || port > 65535 {
			return "", "", 0, ErrPort
		}
	}
	return scheme, host, port, nil
}

func hasDotSegment(p string) bool {
	for _, seg := range strings.Split(p, "/") {
		if seg == "." || seg == ".." {
			return true
		}
	}
	return strings.Contains(p, "\\")
}

// matchesException dit si u vise exactement l'URL SearXNG configurée.
func (g *Guard) matchesException(u *url.URL, scheme, host string, port int) bool {
	ex := g.exception
	if ex == nil || scheme != ex.scheme || host != ex.host || port != ex.port {
		return false
	}
	if hasDotSegment(u.Path) {
		return false
	}
	return ex.path == "" || u.Path == ex.path || strings.HasPrefix(u.Path, ex.path+"/")
}

// check valide une URL avant chaque requête (première et redirections).
// Elle renvoie vrai quand l'URL relève de l'exception SearXNG.
func (g *Guard) check(u *url.URL) (bool, error) {
	scheme, host, port, err := target(u)
	if err != nil {
		return false, err
	}
	if g.matchesException(u, scheme, host, port) {
		return true, nil
	}
	if !g.ports[port] {
		return false, ErrPort
	}
	if ip, err := netip.ParseAddr(host); err == nil && g.forbidden(ip) {
		return false, ErrForbiddenAddress
	}
	return false, nil
}

func (g *Guard) forbidden(a netip.Addr) bool {
	if g.allowAddr != nil && a.IsValid() && g.allowAddr(a.Unmap()) {
		return false
	}
	return Forbidden(a)
}

func (g *Guard) newTransport(dial func(context.Context, string, string) (net.Conn, error)) *http.Transport {
	return &http.Transport{
		Proxy:                  nil, // jamais de mandataire : il contournerait le contrôle du dialer
		DialContext:            dial,
		ForceAttemptHTTP2:      true,
		TLSClientConfig:        &tls.Config{MinVersion: tls.VersionTLS12},
		TLSHandshakeTimeout:    tlsTimeout,
		ResponseHeaderTimeout:  g.timeout,
		MaxResponseHeaderBytes: maxHeaderBytes,
		// Une connexion par requête : aucune connexion ouverte pour
		// l'exception ne peut servir une autre requête.
		DisableKeepAlives: true,
	}
}

type roundTripper struct{ g *Guard }

// RoundTrip revalide chaque requête, y compris chaque saut de redirection,
// puis la confie au transport normal ou à celui de l'exception.
func (rt roundTripper) RoundTrip(r *http.Request) (*http.Response, error) {
	excepted, err := rt.g.check(r.URL)
	if err != nil {
		if r.Body != nil {
			_ = r.Body.Close()
		}
		return nil, err
	}
	if excepted {
		return rt.g.excepted.RoundTrip(r)
	}
	return rt.g.normal.RoundTrip(r)
}

func (g *Guard) checkRedirect(req *http.Request, via []*http.Request) error {
	if len(via) > MaxRedirects {
		return ErrTooManyRedirects
	}
	_, err := g.check(req.URL)
	return err
}

// resolve renvoie les adresses d'un hôte et refuse l'hôte entier si l'une
// d'elles est interdite (pas de choix « au mieux » entre adresses).
func (g *Guard) resolve(ctx context.Context, host string, allowInternal bool) ([]netip.Addr, error) {
	var ips []netip.Addr
	if ip, err := netip.ParseAddr(host); err == nil {
		ips = []netip.Addr{ip}
	} else {
		ips, err = g.resolver.LookupNetIP(ctx, "ip", host)
		if err != nil {
			return nil, fmt.Errorf("%w: %v", ErrUnresolvable, err)
		}
	}
	if len(ips) == 0 {
		return nil, ErrUnresolvable
	}
	if !allowInternal {
		for _, ip := range ips {
			if g.forbidden(ip) {
				return nil, ErrForbiddenAddress
			}
		}
	}
	return ips, nil
}

// control vérifie l'adresse effectivement connectée, juste avant connect(2).
// C'est la défense contre le DNS rebinding : quelle que soit la résolution
// faite plus tôt, une adresse interdite n'est jamais jointe.
func (g *Guard) control(network, address string, _ syscall.RawConn) error {
	ap, err := netip.ParseAddrPort(address)
	if err != nil {
		return fmt.Errorf("%w: %s", ErrForbiddenAddress, address)
	}
	if g.forbidden(ap.Addr()) {
		return ErrForbiddenAddress
	}
	return nil
}

func (g *Guard) dialIPs(ctx context.Context, network string, ips []netip.Addr, port string, control func(string, string, syscall.RawConn) error) (net.Conn, error) {
	d := net.Dialer{Timeout: dialTimeout, Control: control}
	var last error
	for _, ip := range ips {
		c, err := d.DialContext(ctx, network, net.JoinHostPort(ip.Unmap().String(), port))
		if err == nil {
			return c, nil
		}
		last = err
	}
	return nil, last
}

func (g *Guard) dialNormal(ctx context.Context, network, addr string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, ErrURL
	}
	ips, err := g.resolve(ctx, host, false)
	if err != nil {
		return nil, err
	}
	return g.dialIPs(ctx, network, ips, port, g.control)
}

// dialException joint l'hôte SearXNG configuré, adresses internes permises,
// et seulement lui : toute autre cible suit les règles normales.
func (g *Guard) dialException(ctx context.Context, network, addr string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, ErrURL
	}
	ex := g.exception
	if ex == nil || strings.ToLower(host) != ex.host || port != strconv.Itoa(ex.port) {
		return g.dialNormal(ctx, network, addr)
	}
	ips, err := g.resolve(ctx, host, true)
	if err != nil {
		return nil, err
	}
	return g.dialIPs(ctx, network, ips, port, nil)
}

// Client renvoie le client HTTP gardé (construit par outbound.New), pour les
// appels qui doivent gérer eux-mêmes la requête (ex. recherche SearXNG).
// Taille de corps : à plafonner par l'appelant.
func (g *Guard) Client() *http.Client { return g.client }

// FetchPage récupère une page, plafonnée à MaxPageBytes.
func (g *Guard) FetchPage(ctx context.Context, rawURL string) (*Response, error) {
	return g.fetch(ctx, rawURL, g.maxPageBytes, "text/html,application/xhtml+xml;q=0.9,*/*;q=0.1")
}

func (g *Guard) fetch(ctx context.Context, rawURL string, max int64, accept string) (*Response, error) {
	u, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrURL, err)
	}
	if _, err := g.check(u); err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, g.timeout)
	defer cancel()
	req, err := outbound.NewRequest(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrURL, err)
	}
	req.Header.Set("Accept", accept)
	resp, err := g.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return nil, fmt.Errorf("%w: %d", ErrStatus, resp.StatusCode)
	}
	if resp.ContentLength > max {
		return nil, ErrTooLarge
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, max+1))
	if err != nil {
		return nil, err
	}
	if int64(len(body)) > max {
		return nil, ErrTooLarge
	}
	return &Response{
		URL:         resp.Request.URL,
		StatusCode:  resp.StatusCode,
		ContentType: resp.Header.Get("Content-Type"),
		Body:        body,
	}, nil
}
