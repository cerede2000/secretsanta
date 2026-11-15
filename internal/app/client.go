package app

import (
	"net"
	"net/http"
	"net/netip"
	"strings"
)

// clientIP renvoie la vraie adresse du client, pour la limitation des
// tentatives. X-Forwarded-For n'est lu que si la connexion vient d'une
// adresse de bouclage ou privée (le mandataire inverse de l'hôte ou du réseau
// de conteneurs) ; sinon un client pourrait choisir son adresse. Les sauts
// sont lus de droite à gauche : chaque mandataire de confiance ajoute
// l'adresse de son pair à droite, donc l'entrée la plus à droite qui n'est
// pas elle-même de bouclage ou privée est le client ; un jeton illisible
// ramène à l'adresse de la connexion. Si tous les sauts sont privés (client
// sur le réseau local), la plus à droite est retenue.
func clientIP(r *http.Request) string {
	peer := remoteHost(r)
	pa, err := netip.ParseAddr(peer)
	if err != nil || !trustedHop(pa) {
		return peer
	}
	var hops []string
	for _, h := range r.Header.Values("X-Forwarded-For") {
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
		if !trustedHop(a) {
			return a.String()
		}
	}
	if first != "" {
		return first
	}
	return peer
}

// trustedHop indique une adresse de bouclage ou privée (RFC 1918, ULA).
func trustedHop(a netip.Addr) bool {
	a = a.Unmap()
	return a.IsLoopback() || a.IsPrivate()
}

// remoteHost renvoie l'hôte de RemoteAddr.
func remoteHost(r *http.Request) string {
	h, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return h
}
