package code

import (
	"net/http"

	"secretsanta/internal/websec"
)

// ConfirmPageData est donnée à la page du lien de confirmation (GET).
type ConfirmPageData struct {
	GroupID int64
	Preview Preview
	// Token est le jeton du lien, à renvoyer tel quel par le formulaire POST
	// (l'action du formulaire est l'adresse de la page).
	Token string
}

// ConfirmHandler sert le lien de confirmation en un clic. Le GET affiche la
// page (code déjà saisi et bouton « Confirmer mon inscription ») sans rien
// modifier ; seul le POST, protégé par le CSRF de F2, vérifie l'inscription
// et consomme le jeton. Toutes les réponses portent Referrer-Policy
// no-referrer (le jeton est dans l'adresse) et Cache-Control no-store.
type ConfirmHandler struct {
	Service *Service
	// Group résout le groupe de la requête (slug du chemin) ; faux si le
	// groupe est inconnu.
	Group func(r *http.Request) (groupID int64, ok bool)
	// Token lit le jeton dans la requête ; par défaut r.PathValue("token").
	Token func(r *http.Request) string
	// Page affiche la page de confirmation (GET).
	Page func(w http.ResponseWriter, r *http.Request, d ConfirmPageData)
	// Done répond à une vérification réussie (POST) : la suite (création du
	// participant, écran de réussite) appartient à l'appelant.
	Done func(w http.ResponseWriter, r *http.Request, res Result)
	// Refused répond au refus uniforme (jeton invalide, utilisé ou expiré,
	// groupe inconnu) ; par défaut 404 avec le message uniforme.
	Refused func(w http.ResponseWriter, r *http.Request)
}

func noReferrer(w http.ResponseWriter) {
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("Cache-Control", "no-store")
}

func (h ConfirmHandler) token(r *http.Request) string {
	if h.Token != nil {
		return h.Token(r)
	}
	return r.PathValue("token")
}

func (h ConfirmHandler) refuse(w http.ResponseWriter, r *http.Request) {
	if h.Refused != nil {
		h.Refused(w, r)
		return
	}
	http.Error(w, refusedMessage, http.StatusNotFound)
}

// PageHandler renvoie le gestionnaire du GET : il n'a aucun effet sur l'état.
func (h ConfirmHandler) PageHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		noReferrer(w)
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			http.Error(w, "Méthode non autorisée.", http.StatusMethodNotAllowed)
			return
		}
		groupID, ok := h.Group(r)
		if !ok {
			h.refuse(w, r)
			return
		}
		tok := h.token(r)
		p, err := h.Service.Preview(r.Context(), groupID, tok)
		if err != nil {
			h.refuse(w, r)
			return
		}
		h.Page(w, r, ConfirmPageData{GroupID: groupID, Preview: p, Token: tok})
	})
}

// SubmitHandler renvoie le gestionnaire du POST : il n'est jamais atteint par
// une méthode sûre (websec.RequireUnsafeMethod) et passe donc toujours par le
// contrôle CSRF.
func (h ConfirmHandler) SubmitHandler() http.Handler {
	return websec.RequireUnsafeMethod(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		noReferrer(w)
		groupID, ok := h.Group(r)
		if !ok {
			h.refuse(w, r)
			return
		}
		res, err := h.Service.Confirm(r.Context(), groupID, h.token(r))
		if err != nil {
			if IsRefused(err) {
				h.refuse(w, r)
				return
			}
			http.Error(w, "Une erreur interne est survenue. Réessaie plus tard.", http.StatusInternalServerError)
			return
		}
		h.Done(w, r, res)
	}))
}
