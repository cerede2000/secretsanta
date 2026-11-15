package app

import (
	"log/slog"
	"net/http"
)

// statusRecorder retient le statut envoyé au client.
type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (s *statusRecorder) WriteHeader(code int) {
	if s.status == 0 {
		s.status = code
	}
	s.ResponseWriter.WriteHeader(code)
}

func (s *statusRecorder) Write(b []byte) (int, error) {
	if s.status == 0 {
		s.status = http.StatusOK
	}
	return s.ResponseWriter.Write(b)
}

// Unwrap laisse http.ResponseController atteindre le writer d'origine.
func (s *statusRecorder) Unwrap() http.ResponseWriter { return s.ResponseWriter }

// logServerErrors journalise toute réponse 5xx : méthode, route (le motif
// déclaré, jamais le chemin réel qui peut porter un jeton) et statut. La cause
// est journalisée à sa création (errs.Internal) ; ce filet garantit qu'un 500
// n'est jamais muet, même quand la cause n'a pas été typée.
func (a *App) logServerErrors(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec := &statusRecorder{ResponseWriter: w}
		next.ServeHTTP(rec, r)
		if rec.status >= http.StatusInternalServerError {
			slog.Error("réponse d'erreur serveur", "statut", rec.status, "méthode", r.Method, "route", a.routePattern(r))
		}
	})
}

// routePattern renvoie le motif de route qui sert la requête, ou « inconnue ».
func (a *App) routePattern(r *http.Request) string {
	if _, p := a.Mux.Handler(r); p != "" {
		return p
	}
	if _, p := a.Slugs.Handler(r); p != "" {
		return p
	}
	return "inconnue"
}
