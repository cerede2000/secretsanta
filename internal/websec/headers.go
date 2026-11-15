package websec

import "net/http"

// ContentSecurityPolicy est la CSP stricte posée sur toutes les réponses :
// aucun script ni style en ligne, aucune source étrangère, pas d'intégration
// dans un cadre, pas de <base>, pas d'objet.
const ContentSecurityPolicy = "default-src 'self'; script-src 'self'; style-src 'self'; " +
	"form-action 'self'; frame-ancestors 'none'; base-uri 'none'; object-src 'none'"

// PermissionsPolicy coupe les fonctions sensibles du navigateur.
const PermissionsPolicy = "accelerometer=(), camera=(), display-capture=(), " +
	"geolocation=(), gyroscope=(), magnetometer=(), microphone=(), midi=(), payment=(), usb=()"

// HSTS est posé quand PUBLIC_URL est en https. Sans includeSubDomains : le
// domaine parent n'appartient pas forcément à l'instance.
const HSTS = "max-age=31536000"

// SecurityHeaders renvoie le middleware qui pose les en-têtes de sécurité sur
// toutes les réponses, avant l'appel du gestionnaire (erreurs comprises).
func SecurityHeaders(p Policy) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			h := w.Header()
			h.Set("Content-Security-Policy", ContentSecurityPolicy)
			h.Set("X-Content-Type-Options", "nosniff")
			h.Set("Referrer-Policy", "no-referrer")
			h.Set("Permissions-Policy", PermissionsPolicy)
			h.Set("X-Frame-Options", "DENY")
			h.Set("Cross-Origin-Opener-Policy", "same-origin")
			if p.Secure {
				h.Set("Strict-Transport-Security", HSTS)
			}
			next.ServeHTTP(w, r)
		})
	}
}
