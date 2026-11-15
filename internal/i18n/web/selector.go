package web

import (
	"log/slog"
	"net/http"
	"net/url"
	"strings"

	"secretsanta/internal/i18n/negotiate"
	"secretsanta/internal/websec"
)

// Route est le chemin du changement de langue (POST, jeton CSRF exigé).
const Route = "/langue"

// cookieMaxAge : un an.
const cookieMaxAge = 365 * 24 * 3600

// Selector traite le changement de langue.
type Selector struct {
	Cat Translator
	// Secure : cookie Secure (PUBLIC_URL en https).
	Secure bool
	// Participants enregistre la langue du participant connecté (facultatif).
	Participants negotiate.ParticipantLocale
	Logger       *slog.Logger
}

// Handler renvoie le gestionnaire de POST Route. Il refuse toute méthode sûre
// (405) ; le contrôle CSRF est celui du middleware websec.CSRF de la chaîne.
func (s *Selector) Handler() http.Handler {
	return websec.RequireUnsafeMethod(http.HandlerFunc(s.serve))
}

func (s *Selector) serve(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "Requête invalide.", http.StatusBadRequest)
		return
	}
	lang, ok := s.Cat.Match(strings.TrimSpace(r.PostFormValue("lang")))
	if !ok {
		http.Error(w, "Langue inconnue.", http.StatusBadRequest)
		return
	}
	// Le cookie ne contient que le code de langue.
	http.SetCookie(w, &http.Cookie{
		Name:     negotiate.CookieName,
		Value:    lang,
		Path:     "/",
		MaxAge:   cookieMaxAge,
		HttpOnly: true,
		Secure:   s.Secure,
		SameSite: http.SameSiteLaxMode,
	})
	if s.Participants != nil {
		if _, err := s.Participants.RememberLocale(r.Context(), r, lang); err != nil {
			l := s.Logger
			if l == nil {
				l = slog.Default()
			}
			l.Warn("langue du participant non enregistrée", "err", err)
		}
	}
	http.Redirect(w, r, SafeNext(r.PostFormValue("next")), http.StatusSeeOther)
}

// SafeNext ne laisse passer qu'un chemin local (jamais une adresse externe).
func SafeNext(next string) string {
	if next == "" || next[0] != '/' || strings.HasPrefix(next, "//") || strings.ContainsAny(next, "\\\r\n\t") {
		return "/"
	}
	for _, c := range next {
		if c < 0x20 || c == 0x7f {
			return "/"
		}
	}
	if u, err := url.Parse(next); err != nil || u.Host != "" || u.Scheme != "" {
		return "/"
	}
	return next
}
