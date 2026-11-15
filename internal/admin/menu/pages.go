package menu

import (
	"context"
	"errors"
	"net/http"
	"strconv"

	"secretsanta/internal/groups"
	"secretsanta/internal/i18n/negotiate"
	"secretsanta/internal/shared/adminnav"
	"secretsanta/internal/websec"
)

// Noms de gabarits : TemplateLayout est le gabarit à exécuter, il inclut le
// bloc « layout/content » défini par web/templates/admin/menu/menu.html.
const (
	TemplateLayout = "layout/admin"
	NavKey         = "plus"
)

// Destinations des entrées de la feuille « Plus ».
const (
	SettingsPath = "/{slug}/admin/reglages"
	JournalPath  = "/{slug}/admin/journal"
	ExportPath   = "/{slug}/admin/participants/export"
	DataPath     = "/{slug}/admin/donnees"
	HelpPath     = "/{slug}/admin/aide"
	LoginPath    = "/{slug}/admin/connexion"
	LogoutPath   = "/{slug}/admin/deconnexion"
)

// GroupService est ce que les pages demandent au service des groupes.
type GroupService interface {
	BySlug(ctx context.Context, slug string) (*groups.Group, error)
}

// SessionDestroyer termine une session (satisfait par *websec.Sessions).
type SessionDestroyer interface {
	Destroy(ctx context.Context, w http.ResponseWriter, r *http.Request, k websec.Kind) error
}

// Renderer écrit la page name avec le statut donné (fourni par l'orchestrateur).
type Renderer func(w http.ResponseWriter, r *http.Request, status int, name string, data any)

// Deps regroupe les dépendances.
type Deps struct {
	Groups   GroupService
	Sessions SessionDestroyer
	Render   Renderer
	Nav      *adminnav.Registry
	// DefaultLang sert si la langue n'a pas été négociée (« fr » si vide).
	DefaultLang string
}

// Link est une ligne de la feuille « Plus » (clés de texte, jamais de texte).
type Link struct {
	Href     string
	TitleKey string
	HintKey  string
	Icon     string
}

// Page est la donnée du gabarit.
type Page struct {
	Lang     string
	TitleKey string
	Nav      []adminnav.Item
	Slug     string
	Links    []Link
	// LogoutAction est l'action du formulaire de déconnexion.
	LogoutAction string
}

// Pages construit les gestionnaires.
type Pages struct{ d Deps }

// New valide les dépendances.
func New(d Deps) (*Pages, error) {
	if d.Groups == nil || d.Render == nil || d.Sessions == nil || d.Nav == nil {
		return nil, errors.New("menu : Groups, Sessions, Render et Nav sont obligatoires")
	}
	if d.DefaultLang == "" {
		d.DefaultLang = "fr"
	}
	return &Pages{d: d}, nil
}

// Register monte GET /{slug}/admin/plus et POST /{slug}/admin/deconnexion.
func (p *Pages) Register(mux *http.ServeMux) {
	mux.Handle("GET /{slug}/admin/plus", p.Menu())
	mux.Handle("POST /{slug}/admin/deconnexion", p.Logout())
}

func (p *Pages) lang(r *http.Request) string {
	return negotiate.Lang(r.Context(), p.d.DefaultLang)
}

func fill(path, slug string) string {
	return ResolveNav([]adminnav.Item{{Entry: adminnav.Entry{Destination: path}}}, slug)[0].Destination
}

// admin renvoie le groupe de la requête si la session administrateur le
// concerne ; sinon il répond lui-même (404 groupe inconnu, 303 connexion).
func (p *Pages) admin(w http.ResponseWriter, r *http.Request) (*groups.Group, bool) {
	slug := r.PathValue("slug")
	g, err := p.d.Groups.BySlug(r.Context(), slug)
	if err != nil || g == nil {
		http.NotFound(w, r)
		return nil, false
	}
	if _, ok := websec.AdminSession(r.Context(), strconv.FormatInt(g.ID, 10)); !ok {
		http.Redirect(w, r, fill(LoginPath, g.Slug), http.StatusSeeOther)
		return nil, false
	}
	return g, true
}

// Menu sert la feuille « Plus » (lecture seule).
func (p *Pages) Menu() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		g, ok := p.admin(w, r)
		if !ok {
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		links := []Link{
			{Href: fill(SettingsPath, g.Slug), TitleKey: "admin.menu.settings", HintKey: "admin.menu.settings.hint", Icon: "settings"},
			{Href: fill(JournalPath, g.Slug), TitleKey: "admin.menu.journal", HintKey: "admin.menu.journal.hint", Icon: "journal"},
			{Href: fill(ExportPath, g.Slug), TitleKey: "admin.menu.export", HintKey: "admin.menu.export.hint", Icon: "export"},
			{Href: fill(DataPath, g.Slug), TitleKey: "admin.menu.data", HintKey: "admin.menu.data.hint", Icon: "data"},
			{Href: fill(HelpPath, g.Slug), TitleKey: "admin.menu.help", HintKey: "admin.menu.help.hint", Icon: "help"},
		}
		p.d.Render(w, r, http.StatusOK, TemplateLayout, Page{
			Lang:         p.lang(r),
			TitleKey:     "admin.menu.title",
			Nav:          ResolveNav(p.d.Nav.Items(NavKey), g.Slug),
			Slug:         g.Slug,
			Links:        links,
			LogoutAction: fill(LogoutPath, g.Slug),
		})
	})
}

// Logout termine la session administrateur (POST seulement ; le jeton CSRF est
// contrôlé par le middleware de la chaîne).
func (p *Pages) Logout() http.Handler {
	return websec.RequireUnsafeMethod(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		g, ok := p.admin(w, r)
		if !ok {
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		if err := p.d.Sessions.Destroy(r.Context(), w, r, websec.KindAdmin); err != nil {
			http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			return
		}
		http.Redirect(w, r, fill(LoginPath, g.Slug), http.StatusSeeOther)
	}))
}
