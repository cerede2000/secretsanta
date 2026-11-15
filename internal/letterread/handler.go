package letterread

import (
	"context"
	"net/http"
	"strconv"

	"secretsanta/internal/groups"
	"secretsanta/internal/participants"
	"secretsanta/internal/shared/errs"
	"secretsanta/internal/websec"
)

// ImagePath est la seule route de ce paquet : l'image d'une idée de la
// lettre d'une cible, en lecture. Les pages de lecture sont livrées par F5c.
// Aucune route d'écriture, de réponse ou de commentaire n'existe pour le
// tireur.
const ImagePath = "/{slug}/lettre-recue/{cible}/image/{idee}"

// GroupLookup résout l'adresse d'un groupe (satisfait par *groups.Service).
type GroupLookup interface {
	BySlug(ctx context.Context, slug string) (*groups.Group, error)
}

// Route est une route à enregistrer par l'orchestrateur.
type Route struct {
	Method  string
	Pattern string
	Handler http.Handler
}

// Handler sert les routes de lecture du tireur.
type Handler struct {
	svc    *Service
	groups GroupLookup
}

// NewHandler construit les routes de lecture.
func NewHandler(svc *Service, g GroupLookup) *Handler {
	return &Handler{svc: svc, groups: g}
}

// Routes renvoie les routes à enregistrer (toutes en GET), derrière la
// chaîne SecurityHeaders, CSRF, Sessions.
func (h *Handler) Routes() []Route {
	return []Route{{Method: http.MethodGet, Pattern: ImagePath, Handler: readOnly(http.HandlerFunc(h.image))}}
}

// readOnly refuse toute méthode autre que GET et HEAD.
func readOnly(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			http.Error(w, "Méthode non autorisée.", http.StatusMethodNotAllowed)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// actor résout le groupe et la session participant ; ok faux = réponse
// neutre 404 déjà écrite.
func (h *Handler) actor(w http.ResponseWriter, r *http.Request) (participants.Actor, bool) {
	g, err := h.groups.BySlug(r.Context(), r.PathValue("slug"))
	if err != nil || g == nil {
		h.fail(w, err)
		return participants.Actor{}, false
	}
	sess, found := websec.ParticipantSession(r.Context(), strconv.FormatInt(g.ID, 10))
	if !found {
		h.fail(w, nil)
		return participants.Actor{}, false
	}
	pid, err := strconv.ParseInt(sess.Subject, 10, 64)
	if err != nil || pid <= 0 {
		h.fail(w, nil)
		return participants.Actor{}, false
	}
	return participants.ParticipantActor(g.ID, pid), true
}

func (h *Handler) image(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "private, no-store")
	a, ok := h.actor(w, r)
	if !ok {
		return
	}
	pairID, err1 := strconv.ParseInt(r.PathValue("cible"), 10, 64)
	ideaID, err2 := strconv.ParseInt(r.PathValue("idee"), 10, 64)
	if err1 != nil || err2 != nil || pairID <= 0 || ideaID <= 0 {
		h.fail(w, nil)
		return
	}
	b, mime, err := h.svc.Image(r.Context(), a, a.GroupID, pairID, ideaID)
	if err != nil {
		h.fail(w, err)
		return
	}
	w.Header().Set("Content-Type", mime)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Length", strconv.Itoa(len(b)))
	w.WriteHeader(http.StatusOK)
	if r.Method != http.MethodHead {
		_, _ = w.Write(b)
	}
}

// fail écrit une réponse neutre : 404 pour tout refus (groupe, session,
// cible, révélation), 500 pour une panne.
func (h *Handler) fail(w http.ResponseWriter, err error) {
	if err != nil && errs.Is(err, errs.KindInternal) {
		http.Error(w, "Erreur interne.", http.StatusInternalServerError)
		return
	}
	http.Error(w, "Introuvable.", http.StatusNotFound)
}
