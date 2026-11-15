package groups

import (
	"fmt"
	"sort"
	"strings"

	"secretsanta/internal/shared/errs"
)

// reservedSlugs sont interdits comme adresse de groupe : préfixes techniques,
// routes de l'application et noms de fichiers statiques (sans extension ; un
// nom avec point est de toute façon refusé par la syntaxe).
var reservedSlugs = map[string]struct{}{}

func init() {
	for _, s := range []string{
		// Exigés par la spécification.
		"admin", "api", "assets", "static", "health",
		// Routes de l'application et de ses écrans.
		"creer", "creation", "nouveau", "new", "create",
		"groupe", "groupes", "group", "groups",
		"connexion", "deconnexion", "login", "logout", "auth", "session", "sessions", "csrf",
		"inscription", "inscriptions", "desinscription", "unsubscribe", "invitation", "invite", "rejoindre", "join",
		"code", "lien", "liens", "acces",
		"tableau", "participants", "participant", "tirage", "reglages", "plus", "menu", "fiche", "ajuster",
		"delivrabilite", "cible", "attente", "revelation", "lettre", "lettres", "boite", "boite-lettres",
		"reception-seule", "suggestions", "suggestion", "souhaits", "qr", "qrcode",
		// Supervision et technique.
		"healthz", "ready", "readyz", "livez", "metrics", "status", "version", "debug", "internal",
		"well-known", "app", "www", "web", "public", "media", "uploads", "files", "file", "download",
		"mail", "email", "courriel", "smtp", "dkim",
		// Fichiers et dossiers statiques.
		"favicon", "robots", "manifest", "site", "sitemap", "sw", "service-worker", "apple-touch-icon",
		"browserconfig", "humans", "security", "css", "js", "img", "images", "icons", "fonts", "i18n",
		// Pages institutionnelles et rôles.
		"aide", "help", "about", "a-propos", "contact", "mentions-legales", "confidentialite", "privacy",
		"legal", "cgu", "administrateur", "administration", "root", "system", "setup", "install", "config",
	} {
		reservedSlugs[s] = struct{}{}
	}
}

// ReservedSlugs renvoie la liste triée des slugs réservés (copie), pour que
// l'orchestrateur vérifie qu'aucune de ses routes n'y échappe.
func ReservedSlugs() []string {
	out := make([]string, 0, len(reservedSlugs))
	for s := range reservedSlugs {
		out = append(out, s)
	}
	sort.Strings(out)
	return out
}

// IsReservedSlug indique si s est réservé par l'application.
func IsReservedSlug(s string) bool {
	_, ok := reservedSlugs[s]
	return ok
}

// Messages d'erreur sur le slug.
var (
	msgSlugRequired = "L'adresse du groupe est obligatoire."
	msgSlugLength   = fmt.Sprintf("L'adresse du groupe doit compter entre %d et %d caractères.", SlugMinLen, SlugMaxLen)
	msgSlugCharset  = "L'adresse du groupe ne peut contenir que des lettres minuscules sans accent, des chiffres et des tirets (pas de tiret au début, à la fin ni deux à la suite)."
	msgSlugReserved = "Cette adresse est réservée par l'application. Choisissez-en une autre."
	msgSlugTaken    = "Cette adresse est déjà prise. Choisissez-en une autre."
)

// ValidateSlug vérifie la syntaxe d'un slug (espaces de bord retirés, aucune
// autre transformation) et qu'il n'est pas réservé. Elle ne vérifie pas
// l'unicité.
func ValidateSlug(raw string) (string, error) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return "", errs.Validation(msgSlugRequired)
	}
	if len(s) < SlugMinLen || len(s) > SlugMaxLen {
		// Les caractères non ASCII sont refusés plus bas ; len en octets
		// suffit pour rejeter tôt une saisie démesurée.
		if !isSlugSyntax(s) {
			return "", errs.Validation(msgSlugCharset)
		}
		return "", errs.Validation(msgSlugLength)
	}
	if !isSlugSyntax(s) {
		return "", errs.Validation(msgSlugCharset)
	}
	if IsReservedSlug(s) {
		return "", errs.Validation(msgSlugReserved)
	}
	return s, nil
}

// isSlugSyntax : [a-z0-9]+(-[a-z0-9]+)*
func isSlugSyntax(s string) bool {
	if s == "" || s[0] == '-' || s[len(s)-1] == '-' {
		return false
	}
	prevDash := false
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c >= 'a' && c <= 'z', c >= '0' && c <= '9':
			prevDash = false
		case c == '-':
			if prevDash {
				return false
			}
			prevDash = true
		default:
			return false
		}
	}
	return true
}
