package invite

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"net/http"

	"secretsanta/internal/access/limiter"
	"secretsanta/internal/access/magic"
	"secretsanta/internal/websec"
)

// maxFormBytes borne le corps du formulaire.
const maxFormBytes = 16 << 10

// View est ce que la page d'inscription (F11c) affiche. Elle ne contient
// jamais de liste de participants.
type View struct {
	Invite *Invite
	// Result est nil à l'affichage initial (GET).
	Result *Result
	// Closed répète Invite.Closed ou Result.Closed.
	Closed ClosedReason
	// Action est le chemin de soumission du formulaire (même adresse).
	Action string
}

// Renderer rend la page d'inscription avec le statut donné. Fourni par les
// pages (F11c) ; le gabarit doit inclure {{csrfField}} dans le formulaire.
type Renderer interface {
	Render(w http.ResponseWriter, r *http.Request, status int, v View)
}

// Handlers sert /<slug>/rejoindre/<jeton> : GET affiche le formulaire (ou
// le message d'inscriptions closes), POST soumet. Il implémente
// magic.InviteForms pour magic.InvitationPaths.
type Handlers struct {
	Service *Service
	Render  Renderer
	Neutral *magic.Neutral
	Logger  *slog.Logger
	// ClientIP renvoie l'adresse du client (défaut : hôte de RemoteAddr).
	ClientIP func(*http.Request) string
}

var _ magic.InviteForms = (*Handlers)(nil)

func (h *Handlers) logger() *slog.Logger {
	if h.Logger == nil {
		return slog.Default()
	}
	return h.Logger
}

func (h *Handlers) neutral() *magic.Neutral {
	if h.Neutral == nil {
		return magic.NewNeutral(nil)
	}
	return h.Neutral
}

func (h *Handlers) clientIP(r *http.Request) string {
	if h.ClientIP != nil {
		return h.ClientIP(r)
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// noReferrer pose Referrer-Policy: no-referrer et interdit le cache : le
// jeton d'invitation est dans l'adresse.
func noReferrer(w http.ResponseWriter) {
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("Cache-Control", "no-store")
}

// InviteForm implémente magic.InviteForms : nil sans erreur pour un slug
// inconnu, un mode désactivé ou un jeton faux (comparé à temps constant).
func (h *Handlers) InviteForm(ctx context.Context, slug, token string) (http.Handler, error) {
	inv, err := h.Service.Open(ctx, slug, token)
	if errors.Is(err, ErrNeutral) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	submit := websec.RequireUnsafeMethod(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h.submit(w, r, inv, slug, token)
	}))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		noReferrer(w)
		switch r.Method {
		case http.MethodGet, http.MethodHead:
			h.Render.Render(w, r, http.StatusOK, View{Invite: inv, Closed: inv.Closed, Action: r.URL.Path})
		case http.MethodPost:
			submit.ServeHTTP(w, r)
		default:
			w.Header().Set("Allow", "GET, HEAD, POST")
			http.Error(w, "Méthode non autorisée.", http.StatusMethodNotAllowed)
		}
	}), nil
}

// Join renvoie le gestionnaire de /<slug>/rejoindre/<jeton...> (motif
// magic.PatternJoin), réponse neutre comprise.
func (h *Handlers) Join() http.Handler {
	paths := &magic.InvitationPaths{Forms: h, Neutral: h.neutral(), Logger: h.logger()}
	inner := paths.Join()
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		noReferrer(w)
		inner.ServeHTTP(w, r)
	})
}

func (h *Handlers) submit(w http.ResponseWriter, r *http.Request, inv *Invite, slug, token string) {
	r.Body = http.MaxBytesReader(w, r.Body, maxFormBytes)
	if err := r.ParseForm(); err != nil {
		http.Error(w, "Requête invalide.", http.StatusBadRequest)
		return
	}
	sub := Submission{
		FirstName:     r.PostFormValue(FieldFirstName),
		Name:          r.PostFormValue(FieldName),
		Email:         r.PostFormValue(FieldEmail),
		Honeypot:      r.PostFormValue(FieldHoneypot),
		ConfirmShared: r.PostFormValue(FieldConfirmShared) == "1",
		IP:            h.clientIP(r),
	}
	res, err := h.Service.Submit(r.Context(), slug, token, sub)
	switch {
	case errors.Is(err, ErrNeutral):
		h.neutral().ServeHTTP(w, r)
		return
	case errors.Is(err, limiter.ErrLimited):
		http.Error(w, limiter.Error().Message, http.StatusTooManyRequests)
		return
	case err != nil:
		h.logger().Error("inscription", "erreur", err)
		http.Error(w, "Une erreur interne est survenue. Réessayez plus tard.", http.StatusInternalServerError)
		return
	}
	status := http.StatusOK
	if res.Outcome == OutcomeInvalid || res.Outcome == OutcomeNameTaken {
		status = http.StatusUnprocessableEntity
	}
	// L'état affiché est celui de la soumission : une inscription acceptée et
	// un champ piège rempli rendent exactement la même vue.
	shown := *inv
	shown.Closed = res.Closed
	h.Render.Render(w, r, status, View{Invite: &shown, Result: res, Closed: res.Closed, Action: r.URL.Path})
}
