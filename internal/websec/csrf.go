package websec

import (
	"context"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"html/template"
	"net/http"
	"strings"

	"secretsanta/internal/shared/random"
)

const (
	// CSRFFieldName est le nom du champ caché des formulaires.
	CSRFFieldName = "csrf_token"
	// CSRFHeaderName est l'en-tête des appels JavaScript.
	CSRFHeaderName = "X-CSRF-Token"
	// CSRFRejectMessage est le message français renvoyé avec le refus 403.
	CSRFRejectMessage = "La requête a été refusée : le formulaire a expiré ou provient d'un autre site. Recharge la page et réessaie."

	csrfCookieBase = "ss_csrf"
	csrfTokenBytes = 32
)

// Raisons de refus (utiles aux journaux de l'orchestrateur).
var (
	ErrCSRFOrigin = errors.New("websec: origine de la requête refusée")
	ErrCSRFToken  = errors.New("websec: jeton CSRF absent ou invalide")
)

type csrfCtxKey struct{}

// CSRFOptions configure la protection CSRF.
type CSRFOptions struct {
	// Exempt désigne les seules routes dispensées (celles qui portent leur
	// propre signature HMAC, par exemple le désabonnement en un clic). Nil :
	// aucune exemption.
	Exempt func(*http.Request) bool
	// OnReject rend la page de refus. Aucun statut n'est encore écrit quand il
	// est appelé : il doit écrire lui-même un 403. Nil : texte brut avec
	// CSRFRejectMessage.
	OnReject func(w http.ResponseWriter, r *http.Request, reason error)
}

// CSRF protège les requêtes modifiantes : jeton lié à un cookie HttpOnly
// (double soumission masquée) et contrôle de l'origine contre PUBLIC_URL.
type CSRF struct {
	policy Policy
	opts   CSRFOptions
}

// NewCSRF crée la protection CSRF.
func NewCSRF(p Policy, opts CSRFOptions) *CSRF {
	return &CSRF{policy: p, opts: opts}
}

// isSafeMethod : méthodes qui ne doivent jamais modifier l'état.
func isSafeMethod(m string) bool {
	switch m {
	case http.MethodGet, http.MethodHead, http.MethodOptions, http.MethodTrace:
		return true
	}
	return false
}

// Middleware pose le cookie CSRF s'il manque, met le jeton à disposition des
// gabarits et refuse en 403 toute requête modifiante (toute méthode autre que
// GET, HEAD, OPTIONS, TRACE) sans origine valide ou sans jeton valide.
func (c *CSRF) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		secret, ok := c.readCookie(r)
		if !ok {
			b, err := random.Bytes(csrfTokenBytes)
			if err != nil {
				http.Error(w, "Une erreur interne est survenue. Réessaie plus tard.", http.StatusInternalServerError)
				return
			}
			secret = b
			// Cookie de session du navigateur, SameSite=Lax : présent à l'arrivée
			// par un lien de courriel, absent des envois croisés.
			http.SetCookie(w, c.policy.cookie(csrfCookieBase, base64.RawURLEncoding.EncodeToString(b), http.SameSiteLaxMode, 0))
		}
		r = r.WithContext(context.WithValue(r.Context(), csrfCtxKey{}, secret))

		if isSafeMethod(r.Method) || (c.opts.Exempt != nil && c.opts.Exempt(r)) {
			next.ServeHTTP(w, r)
			return
		}
		if !c.originAllowed(r) {
			c.reject(w, r, ErrCSRFOrigin)
			return
		}
		// Un cookie qui vient d'être créé ne peut correspondre à aucun jeton.
		if !ok || !tokenMatches(secret, submittedToken(r)) {
			c.reject(w, r, ErrCSRFToken)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (c *CSRF) readCookie(r *http.Request) ([]byte, bool) {
	ck, err := r.Cookie(c.policy.cookieName(csrfCookieBase))
	if err != nil {
		return nil, false
	}
	b, err := base64.RawURLEncoding.DecodeString(ck.Value)
	if err != nil || len(b) != csrfTokenBytes {
		return nil, false
	}
	return b, true
}

// originAllowed contrôle l'origine de la requête modifiante :
//   - Sec-Fetch-Site, s'il est présent, doit valoir same-origin ;
//   - Origin, s'il est présent et non « null », doit être PUBLIC_URL ;
//   - au moins un des deux doit attester l'origine. Avec Referrer-Policy:
//     no-referrer, un navigateur envoie « Origin: null » sur un POST de même
//     origine : Sec-Fetch-Site tranche alors.
func (c *CSRF) originAllowed(r *http.Request) bool {
	attested := false
	if sfs := r.Header.Values("Sec-Fetch-Site"); len(sfs) > 0 {
		if len(sfs) != 1 || strings.ToLower(strings.TrimSpace(sfs[0])) != "same-origin" {
			return false
		}
		attested = true
	}
	if origins := r.Header.Values("Origin"); len(origins) > 0 {
		if len(origins) != 1 {
			return false
		}
		o := strings.ToLower(strings.TrimSpace(origins[0]))
		if o != "null" {
			if o != c.policy.Origin {
				return false
			}
			attested = true
		}
	}
	return attested
}

// submittedToken lit l'en-tête X-CSRF-Token, sinon le champ du corps de
// formulaire (lu par net/http pour POST, PUT et PATCH seulement : un DELETE
// passe donc par l'en-tête).
func submittedToken(r *http.Request) string {
	if t := r.Header.Get(CSRFHeaderName); t != "" {
		return t
	}
	return r.PostFormValue(CSRFFieldName)
}

func (c *CSRF) reject(w http.ResponseWriter, r *http.Request, reason error) {
	if c.opts.OnReject != nil {
		c.opts.OnReject(w, r, reason)
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusForbidden)
	_, _ = w.Write([]byte(CSRFRejectMessage + "\n"))
}

// maskToken masque le secret par un masque aléatoire neuf (le jeton change à
// chaque rendu : pas d'attaque par compression du type BREACH).
func maskToken(secret []byte) (string, error) {
	mask, err := random.Bytes(len(secret))
	if err != nil {
		return "", err
	}
	out := make([]byte, 2*len(secret))
	copy(out, mask)
	for i := range secret {
		out[len(secret)+i] = mask[i] ^ secret[i]
	}
	return base64.RawURLEncoding.EncodeToString(out), nil
}

func tokenMatches(secret []byte, token string) bool {
	if token == "" {
		return false
	}
	raw, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil || len(raw) != 2*len(secret) {
		return false
	}
	n := len(secret)
	plain := make([]byte, n)
	for i := 0; i < n; i++ {
		plain[i] = raw[i] ^ raw[n+i]
	}
	return subtle.ConstantTimeCompare(plain, secret) == 1
}

// Token renvoie un jeton CSRF masqué pour la requête (vide si le middleware
// CSRF n'a pas traité la requête).
func Token(r *http.Request) (string, error) {
	secret, _ := r.Context().Value(csrfCtxKey{}).([]byte)
	if len(secret) != csrfTokenBytes {
		return "", errors.New("websec: middleware CSRF absent de la chaîne")
	}
	return maskToken(secret)
}

// Field renvoie le champ caché à placer dans chaque formulaire.
func Field(r *http.Request) (template.HTML, error) {
	t, err := Token(r)
	if err != nil {
		return "", err
	}
	return template.HTML(`<input type="hidden" name="` + CSRFFieldName + `" value="` + template.HTMLEscapeString(t) + `">`), nil
}

// TemplateFuncs renvoie les fonctions de gabarit liées à la requête :
// csrfField (champ caché des formulaires) et csrfToken (valeur pour l'en-tête
// X-CSRF-Token des appels JavaScript, par exemple dans une balise meta).
// L'orchestrateur les applique sur un clone du gabarit pour chaque requête.
func TemplateFuncs(r *http.Request) template.FuncMap {
	return template.FuncMap{
		"csrfField": func() (template.HTML, error) { return Field(r) },
		"csrfToken": func() (string, error) { return Token(r) },
	}
}

// TemplateFuncsPlaceholder déclare csrfField et csrfToken au moment de
// l'analyse des gabarits ; leur exécution sans TemplateFuncs échoue.
func TemplateFuncsPlaceholder() template.FuncMap {
	fail := errors.New("websec: TemplateFuncs(r) non appliqué au gabarit")
	return template.FuncMap{
		"csrfField": func() (template.HTML, error) { return "", fail },
		"csrfToken": func() (string, error) { return "", fail },
	}
}

// RequireUnsafeMethod garantit qu'un gestionnaire modifiant l'état n'est
// jamais atteint par GET, HEAD, OPTIONS ou TRACE (405) : il passe donc
// toujours par le contrôle CSRF.
func RequireUnsafeMethod(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if isSafeMethod(r.Method) {
			w.Header().Set("Allow", "POST, PUT, PATCH, DELETE")
			http.Error(w, "Méthode non autorisée.", http.StatusMethodNotAllowed)
			return
		}
		next.ServeHTTP(w, r)
	})
}
