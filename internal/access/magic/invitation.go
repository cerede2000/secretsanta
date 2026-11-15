package magic

import (
	"context"
	"log/slog"
	"net/http"
	"strconv"
)

// Motifs de route proposés à l'orchestrateur (http.ServeMux, Go 1.22+). Le
// motif à reste {jeton...} capte aussi un jeton vide (« /<slug>/rejoindre/ »)
// et un jeton contenant des barres obliques ; le motif nu capte
// « /<slug>/rejoindre ».
const (
	PatternPublic   = "GET /{slug}"
	PatternJoin     = "/{slug}/rejoindre/{jeton...}"
	PatternJoinBare = "/{slug}/rejoindre"
)

// defaultNeutralBody est le corps par défaut de la réponse neutre.
const defaultNeutralBody = `<!doctype html>
<html lang="fr">
<head><meta charset="utf-8"><meta name="viewport" content="width=device-width, initial-scale=1"><title>Page introuvable</title></head>
<body><main><h1>Page introuvable</h1><p>Cette page n'existe pas ou n'est plus disponible.</p></main></body>
</html>
`

// Neutral est l'unique réponse neutre : même statut, même corps, mêmes
// en-têtes pour un slug inconnu, un jeton absent, invalide ou renouvelé et un
// mode d'inscription désactivé.
type Neutral struct {
	status int
	body   []byte
	header http.Header
}

// NewNeutral crée la réponse neutre (404) avec le corps HTML donné ; un corps
// vide prend le corps par défaut.
func NewNeutral(body []byte) *Neutral {
	if len(body) == 0 {
		body = []byte(defaultNeutralBody)
	}
	b := append([]byte(nil), body...)
	h := http.Header{}
	h.Set("Content-Type", "text/html; charset=utf-8")
	h.Set("Content-Length", strconv.Itoa(len(b)))
	h.Set("Cache-Control", "no-store")
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Referrer-Policy", "no-referrer")
	return &Neutral{status: http.StatusNotFound, body: b, header: h}
}

// ServeHTTP écrit la réponse neutre.
func (n *Neutral) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	dst := w.Header()
	for k, v := range n.header {
		dst[k] = append([]string(nil), v...)
	}
	w.WriteHeader(n.status)
	if r.Method != http.MethodHead {
		_, _ = w.Write(n.body)
	}
}

// PublicPages fournit la page publique d'un groupe (F2/F2e). Elle renvoie
// nil sans erreur pour un slug inconnu. Elle ne doit pas dépendre du mode
// d'inscription.
type PublicPages interface {
	PublicPage(ctx context.Context, slug string) (http.Handler, error)
}

// InviteForms fournit le formulaire d'inscription (F11) pour un slug et un
// jeton. Elle renvoie nil sans erreur, sans dire pourquoi, si le slug est
// inconnu, si le mode est désactivé ou si le jeton ne correspond pas au jeton
// en vigueur (comparaison à temps constant) ; jamais d'erreur typée qui
// distinguerait ces cas.
type InviteForms interface {
	InviteForm(ctx context.Context, slug, token string) (http.Handler, error)
}

// InvitationPaths sert la page publique /<slug> et le chemin
// /<slug>/rejoindre/<jeton> en ne révélant ni l'existence du groupe au-delà
// de sa page publique, ni l'état du mode d'inscription.
type InvitationPaths struct {
	Pages   PublicPages
	Forms   InviteForms
	Neutral *Neutral
	Logger  *slog.Logger
}

func (p *InvitationPaths) neutral() *Neutral {
	if p.Neutral == nil {
		return NewNeutral(nil)
	}
	return p.Neutral
}

func (p *InvitationPaths) logger() *slog.Logger {
	if p.Logger == nil {
		return slog.Default()
	}
	return p.Logger
}

// Public sert /<slug> (valeur de chemin « slug ») : la page publique du
// groupe, ou la réponse neutre si le slug est inconnu. Une erreur du
// fournisseur est journalisée et donne la réponse neutre.
func (p *InvitationPaths) Public() http.Handler {
	n := p.neutral()
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		slug := r.PathValue("slug")
		if slug == "" || p.Pages == nil {
			n.ServeHTTP(w, r)
			return
		}
		h, err := p.Pages.PublicPage(r.Context(), slug)
		if err != nil {
			p.logger().Error("page publique", "erreur", err)
		}
		if err != nil || h == nil {
			n.ServeHTTP(w, r)
			return
		}
		h.ServeHTTP(w, r)
	})
}

// Join sert /<slug>/rejoindre/<jeton> (valeurs de chemin « slug » et
// « jeton ») : le formulaire d'inscription si F11 le délivre, sinon la
// réponse neutre. Un jeton absent ne consulte pas F11. Une erreur du
// fournisseur est journalisée et donne la réponse neutre.
func (p *InvitationPaths) Join() http.Handler {
	n := p.neutral()
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		slug, token := r.PathValue("slug"), r.PathValue("jeton")
		if slug == "" || token == "" || p.Forms == nil {
			n.ServeHTTP(w, r)
			return
		}
		h, err := p.Forms.InviteForm(r.Context(), slug, token)
		if err != nil {
			p.logger().Error("chemin d'invitation", "erreur", err)
		}
		if err != nil || h == nil {
			n.ServeHTTP(w, r)
			return
		}
		h.ServeHTTP(w, r)
	})
}
