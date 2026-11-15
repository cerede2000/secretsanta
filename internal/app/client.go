package app

import (
	"net"
	"net/http"
	"net/netip"
	"strings"

	"secretsanta/internal/config"
)

// clientIPResolver retrouve l'adresse réelle du client, pour la limitation
// des tentatives, selon TRUSTED_PROXIES et CLIENT_IP_HEADER.
type clientIPResolver struct {
	trusted []netip.Prefix // vide : bouclage et privées
	none    bool
	header  string
}

// newClientIP construit la fonction de résolution depuis la configuration ;
// la valeur zéro donne le comportement par défaut. Un en-tête non reconnu est
// une erreur.
func newClientIP(c config.ClientIP) (func(*http.Request) string, error) {
	h, err := config.ParseClientIPHeader(c.Header)
	if err != nil {
		return nil, err
	}
	res := &clientIPResolver{trusted: c.Trusted, none: c.None, header: h}
	return res.resolve, nil
}

// isTrusted indique si l'adresse est un mandataire de confiance.
func (c *clientIPResolver) isTrusted(a netip.Addr) bool {
	a = a.Unmap()
	if c.none {
		return false
	}
	if len(c.trusted) == 0 {
		return a.IsLoopback() || a.IsPrivate()
	}
	for _, p := range c.trusted {
		if p.Contains(a) {
			return true
		}
	}
	return false
}

// resolve renvoie l'adresse du client. L'en-tête n'est lu que si la connexion
// vient d'un mandataire de confiance ; sinon un client pourrait choisir son
// adresse. Pour X-Forwarded-For, les sauts sont lus de droite à gauche :
// chaque mandataire de confiance ajoute l'adresse de son pair à droite, donc
// l'entrée la plus à droite qui n'est pas de confiance est le client ; un
// jeton illisible ramène à l'adresse de la connexion. Si tous les sauts sont
// de confiance, la plus à droite est retenue. X-Real-IP et CF-Connecting-IP
// portent une valeur unique.
func (c *clientIPResolver) resolve(r *http.Request) string {
	peer := remoteHost(r)
	pa, err := netip.ParseAddr(peer)
	if err != nil || !c.isTrusted(pa) {
		return peer
	}
	if c.header != config.HeaderXForwardedFor {
		a, err := netip.ParseAddr(strings.TrimSpace(r.Header.Get(c.header)))
		if err != nil {
			return peer
		}
		return a.Unmap().String()
	}
	var hops []string
	for _, h := range r.Header.Values(config.HeaderXForwardedFor) {
		for _, e := range strings.Split(h, ",") {
			hops = append(hops, strings.TrimSpace(e))
		}
	}
	first := ""
	for i := len(hops) - 1; i >= 0; i-- {
		a, err := netip.ParseAddr(hops[i])
		if err != nil {
			return peer
		}
		a = a.Unmap()
		if first == "" {
			first = a.String()
		}
		if !c.isTrusted(a) {
			return a.String()
		}
	}
	if first != "" {
		return first
	}
	return peer
}

// remoteHost renvoie l'hôte de RemoteAddr.
func remoteHost(r *http.Request) string {
	h, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return h
}
