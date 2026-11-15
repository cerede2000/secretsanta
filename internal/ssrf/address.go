// Package ssrf est la garde unique contre le SSRF pour toute récupération de
// page ou d'image, et la copie locale des images vérifiées.
//
// La garde est construite sur la fabrique de client sortant (outbound.New) :
// schémas http/https seulement, ports restreints, résolution DNS puis refus des
// adresses internes, contrôle de l'adresse effectivement connectée dans le
// dialer, redirections limitées et revalidées, taille et délai plafonnés.
package ssrf

import (
	"net/netip"
)

// blockedPrefixes liste toutes les plages refusées. Une adresse IPv4
// encapsulée (::ffff:a.b.c.d) est ramenée à IPv4 avant la comparaison.
var blockedPrefixes = mustPrefixes(
	// IPv4
	"0.0.0.0/8",        // « ce réseau »
	"10.0.0.0/8",       // privé
	"100.64.0.0/10",    // CGNAT (contient 100.100.100.200, métadonnées Alibaba)
	"127.0.0.0/8",      // boucle locale
	"169.254.0.0/16",   // lien local (contient 169.254.169.254, métadonnées AWS/GCP/Azure/OpenStack)
	"172.16.0.0/12",    // privé
	"192.0.0.0/24",     // affectations IETF (contient 192.0.0.192, métadonnées Oracle)
	"192.0.2.0/24",     // documentation
	"192.88.99.0/24",   // relais 6to4
	"192.168.0.0/16",   // privé
	"198.18.0.0/15",    // bancs d'essai
	"198.51.100.0/24",  // documentation
	"203.0.113.0/24",   // documentation
	"224.0.0.0/4",      // multidiffusion
	"240.0.0.0/4",      // réservé et diffusion 255.255.255.255
	"168.63.129.16/32", // hôte virtuel Azure (WireServer), adresse publique mais interne
	// IPv6
	"::/96",          // non spécifiée, boucle et IPv4 compatibles (obsolètes)
	"::ffff:0:0/96",  // IPv4 encapsulées (normalement déjà ramenées à IPv4)
	"64:ff9b::/96",   // NAT64 : cache une IPv4 quelconque
	"64:ff9b:1::/48", // NAT64 local
	"100::/64",       // poubelle
	"2001::/23",      // affectations IETF (Teredo, ORCHID…)
	"2001:db8::/32",  // documentation
	"2002::/16",      // 6to4 : cache une IPv4 quelconque
	"3fff::/20",      // documentation
	"5f00::/16",      // SRv6
	"fc00::/7",       // adresses locales uniques (contient fd00:ec2::254, métadonnées AWS)
	"fe80::/10",      // lien local
	"fec0::/10",      // site local (obsolète)
	"ff00::/8",       // multidiffusion
)

func mustPrefixes(ss ...string) []netip.Prefix {
	out := make([]netip.Prefix, 0, len(ss))
	for _, s := range ss {
		out = append(out, netip.MustParsePrefix(s))
	}
	return out
}

// Forbidden indique si une adresse ne doit jamais être jointe : privée,
// locale, de lien local, de métadonnées cloud, réservée, multidiffusion, ou
// équivalent IPv6. Une adresse invalide ou portant une zone est refusée.
func Forbidden(a netip.Addr) bool {
	if !a.IsValid() || a.Zone() != "" {
		return true
	}
	a = a.Unmap()
	if a.Is6() {
		// Seule la plage unicast globale 2000::/3 est joignable.
		if !netip.MustParsePrefix("2000::/3").Contains(a) {
			return true
		}
	}
	if !a.IsGlobalUnicast() || a.IsPrivate() || a.IsLoopback() || a.IsLinkLocalUnicast() ||
		a.IsLinkLocalMulticast() || a.IsInterfaceLocalMulticast() || a.IsMulticast() || a.IsUnspecified() {
		return true
	}
	for _, p := range blockedPrefixes {
		if p.Contains(a) {
			return true
		}
	}
	return false
}
