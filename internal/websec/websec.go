// Package websec pose les protections web communes à toutes les pages :
// sessions (administrateur de groupe et participant) avec cookies durcis,
// protection CSRF des requêtes modifiantes avec contrôle d'origine, en-têtes
// de sécurité et CSP stricte.
//
// Le paquet ne connaît ni les groupes ni les participants : les gestionnaires
// de connexion lui confient l'identité à retenir (identifiants opaques).
// L'orchestrateur (internal/app) branche les middlewares, les fonctions de
// gabarit et les migrations.
package websec

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

// Policy est dérivée de PUBLIC_URL : origine attendue des requêtes
// modifiantes et usage de l'attribut Secure, du préfixe __Host- et de HSTS.
type Policy struct {
	// Origin est l'origine publique sérialisée (schéma://hôte[:port]), en minuscules.
	Origin string
	// Secure est vrai quand PUBLIC_URL est en https.
	Secure bool
}

// NewPolicy dérive la politique de PUBLIC_URL (déjà validée par la configuration).
func NewPolicy(publicURL string) (Policy, error) {
	u, err := url.Parse(strings.TrimSpace(publicURL))
	if err != nil {
		return Policy{}, fmt.Errorf("websec: PUBLIC_URL invalide : %w", err)
	}
	scheme := strings.ToLower(u.Scheme)
	if (scheme != "http" && scheme != "https") || u.Host == "" {
		return Policy{}, fmt.Errorf("websec: PUBLIC_URL doit être une adresse http(s) complète")
	}
	host := strings.ToLower(u.Host)
	// Le port par défaut n'apparaît pas dans l'en-tête Origin.
	if scheme == "https" {
		host = strings.TrimSuffix(host, ":443")
	} else {
		host = strings.TrimSuffix(host, ":80")
	}
	return Policy{Origin: scheme + "://" + host, Secure: scheme == "https"}, nil
}

// cookieName préfixe le nom par __Host- en https : le cookie est alors
// garanti Secure, sans Domain et de chemin « / » (pas d'injection depuis un
// sous-domaine).
func (p Policy) cookieName(base string) string {
	if p.Secure {
		return "__Host-" + base
	}
	return base
}

// cookie construit un cookie durci : HttpOnly toujours, Path « / », Secure
// selon PUBLIC_URL.
func (p Policy) cookie(base, value string, sameSite http.SameSite, maxAge int) *http.Cookie {
	return &http.Cookie{
		Name:     p.cookieName(base),
		Value:    value,
		Path:     "/",
		HttpOnly: true,
		Secure:   p.Secure,
		SameSite: sameSite,
		MaxAge:   maxAge,
	}
}
