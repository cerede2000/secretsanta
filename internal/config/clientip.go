package config

import (
	"fmt"
	"net/netip"
	"strings"
)

// En-têtes acceptés pour CLIENT_IP_HEADER.
const (
	HeaderXForwardedFor = "X-Forwarded-For"
	HeaderXRealIP       = "X-Real-IP"
	HeaderCFConnecting  = "CF-Connecting-IP"
)

// ClientIP décrit comment retrouver l'adresse réelle du client derrière un
// mandataire inverse. La valeur zéro donne le comportement par défaut :
// bouclage et réseaux privés de confiance, en-tête X-Forwarded-For.
type ClientIP struct {
	// Trusted liste les réseaux dont on croit l'en-tête. Vide (et None faux) :
	// bouclage et adresses privées.
	Trusted []netip.Prefix
	// None : ne jamais croire d'en-tête, l'adresse du pair est retenue.
	None bool
	// Header est l'en-tête lu ; vide vaut X-Forwarded-For.
	Header string
}

// ParseClientIPHeader valide CLIENT_IP_HEADER (insensible à la casse) et
// renvoie le nom canonique ; vide donne X-Forwarded-For.
func ParseClientIPHeader(v string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "", "x-forwarded-for":
		return HeaderXForwardedFor, nil
	case "x-real-ip":
		return HeaderXRealIP, nil
	case "cf-connecting-ip":
		return HeaderCFConnecting, nil
	}
	return "", fmt.Errorf("%s doit valoir %s, %s ou %s.", VarClientIPHeader, HeaderXForwardedFor, HeaderXRealIP, HeaderCFConnecting)
}

// ParseTrustedProxies analyse TRUSTED_PROXIES : adresses ou réseaux CIDR
// séparés par des virgules, ou « none ». Vide : défaut (liste vide, none faux).
func ParseTrustedProxies(v string) (prefixes []netip.Prefix, none bool, err error) {
	v = strings.TrimSpace(v)
	if v == "" {
		return nil, false, nil
	}
	if strings.EqualFold(v, "none") {
		return nil, true, nil
	}
	for _, e := range strings.Split(v, ",") {
		e = strings.TrimSpace(e)
		if e == "" {
			return nil, false, fmt.Errorf("%s contient une entrée vide : séparer des adresses ou réseaux CIDR par des virgules, ou indiquer none.", VarTrustedProxies)
		}
		if strings.EqualFold(e, "none") {
			return nil, false, fmt.Errorf("%s : none ne se combine pas avec d'autres entrées.", VarTrustedProxies)
		}
		if p, perr := netip.ParsePrefix(e); perr == nil {
			prefixes = append(prefixes, p.Masked())
			continue
		}
		a, aerr := netip.ParseAddr(e)
		if aerr != nil {
			return nil, false, fmt.Errorf("%s : « %s » n'est ni une adresse IP ni un réseau CIDR (par exemple 10.0.0.0/8).", VarTrustedProxies, e)
		}
		a = a.Unmap()
		prefixes = append(prefixes, netip.PrefixFrom(a, a.BitLen()))
	}
	return prefixes, false, nil
}

func (l *loader) loadClientIP(c *Config) {
	h, err := ParseClientIPHeader(l.get(VarClientIPHeader))
	if err != nil {
		l.fail("%s", err.Error())
	}
	p, none, err := ParseTrustedProxies(l.get(VarTrustedProxies))
	if err != nil {
		l.fail("%s", err.Error())
	}
	c.ClientIP = ClientIP{Trusted: p, None: none, Header: h}
}
