// Package grouppages porte les pages publiques des groupes (F2e) : accueil
// d'un groupe (/<slug>, nom seulement, jamais de participant) et création
// d'un groupe avec affichage unique du secret d'organisateur.
//
// Le paquet ne connaît ni les routes ni l'analyse des gabarits : l'orchestrateur
// fournit un Renderer et appelle Register (ou monte les gestionnaires).
package grouppages

import (
	"context"
	"errors"
	"net"
	"net/http"
	"strings"
	"time"

	"secretsanta/internal/groups"
	"secretsanta/internal/i18n/negotiate"
	"secretsanta/internal/shared/errs"
	"secretsanta/internal/websec"
)

// Noms des gabarits (web/templates/groups/*.html) à analyser avec le layout.
const (
	TemplateHome    = "groups/home"
	TemplateCreate  = "groups/create"
	TemplateCreated = "groups/created"
	TemplateError   = "groups/error"
)

// Chemins publics.
const (
	CreatePath = "/creer"
	// maxFormBytes borne le corps d'un formulaire de création.
	maxFormBytes = 64 << 10
	// revealLayout est le format d'un champ datetime-local.
	revealLayout = "2006-01-02T15:04"
)

// GroupService est ce que les pages demandent au service des groupes
// (satisfait par *groups.Service).
type GroupService interface {
	PublicPage(ctx context.Context, slug string) (groups.PublicView, error)
	Create(ctx context.Context, in groups.CreateInput) (*groups.CreateResult, error)
}

// Renderer écrit la page name avec le statut donné. L'orchestrateur le fournit :
// il clone les gabarits, applique t/tn (i18n/web.Funcs) et csrfField
// (websec.TemplateFuncs), puis exécute le gabarit nommé.
type Renderer func(w http.ResponseWriter, r *http.Request, status int, name string, data any)

// Deps regroupe les dépendances des pages.
type Deps struct {
	Groups GroupService
	Render Renderer
	// DefaultLang sert si la langue n'a pas été négociée (« fr » si vide).
	DefaultLang string
	// PublicURL (sans barre finale) préfixe l'adresse du groupe affichée à
	// la création ; vide : adresse relative.
	PublicURL string
	// Location sert à lire la date de révélation (UTC si nil).
	Location *time.Location
	// AdminPath donne le chemin de connexion administrateur d'un groupe
	// (« /<slug>/admin » si nil).
	AdminPath func(slug string) string
	// ClientKey identifie le client pour la limitation des tentatives
	// (hôte de RemoteAddr si nil).
	ClientKey func(r *http.Request) string
}

// Form est la saisie conservée quand le formulaire est ré-affiché. Le mot de
// passe d'instance n'y figure jamais.
type Form struct {
	Name      string
	Slug      string
	Budget    string
	RevealAt  string
	EventDate string
	AdminView bool
}

// Page est la donnée de tous les gabarits du paquet.
type Page struct {
	Lang     string
	TitleKey string
	// ErrorKey est la clé de texte du message d'erreur intégré à la page.
	ErrorKey string
	Name     string
	Slug     string
	Form     Form
	// Renseignés une seule fois, à la création.
	Secret    string
	GroupURL  string
	AdminPath string
}

// Pages construit les gestionnaires.
type Pages struct{ d Deps }

// New valide les dépendances.
func New(d Deps) (*Pages, error) {
	if d.Groups == nil || d.Render == nil {
		return nil, errors.New("grouppages : Groups et Render sont obligatoires")
	}
	if d.DefaultLang == "" {
		d.DefaultLang = "fr"
	}
	if d.Location == nil {
		d.Location = time.UTC
	}
	d.PublicURL = strings.TrimRight(d.PublicURL, "/")
	if d.AdminPath == nil {
		d.AdminPath = func(slug string) string { return "/" + slug + "/admin" }
	}
	if d.ClientKey == nil {
		d.ClientKey = remoteHost
	}
	return &Pages{d: d}, nil
}

func remoteHost(r *http.Request) string {
	h, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return h
}

// Register monte les routes : GET /creer, POST /creer (protégé par
// RequireUnsafeMethod) et GET /{slug}. Les routes plus spécifiques
// (/creer, /connexion, /static/...) l'emportent sur /{slug} dans le mux.
func (p *Pages) Register(mux *http.ServeMux) {
	mux.Handle("GET "+CreatePath, p.CreateForm())
	mux.Handle("POST "+CreatePath, p.CreateSubmit())
	mux.Handle("GET /{slug}", p.Home())
}

func (p *Pages) lang(r *http.Request) string {
	return negotiate.Lang(r.Context(), p.d.DefaultLang)
}

func noStore(w http.ResponseWriter) { w.Header().Set("Cache-Control", "no-store") }

// Home sert l'accueil public d'un groupe : nom et accueil, jamais la liste des
// participants. Un groupe inconnu reçoit la page neutre 404.
func (p *Pages) Home() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		view, err := p.d.Groups.PublicPage(r.Context(), r.PathValue("slug"))
		if err != nil {
			status, key := http.StatusNotFound, "core.errors.not_found"
			if !errs.Is(err, errs.KindNotFound) {
				status, key = http.StatusInternalServerError, "core.errors.internal"
			}
			p.d.Render(w, r, status, TemplateError, Page{Lang: p.lang(r), TitleKey: "groups.error.title", ErrorKey: key})
			return
		}
		p.d.Render(w, r, http.StatusOK, TemplateHome, Page{Lang: p.lang(r), TitleKey: "groups.home.title", Name: view.Name, Slug: view.Slug})
	})
}

// CreateForm sert le formulaire de création (GET, ne modifie rien).
func (p *Pages) CreateForm() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p.d.Render(w, r, http.StatusOK, TemplateCreate, Page{Lang: p.lang(r), TitleKey: "groups.create.title"})
	})
}

// CreateSubmit traite le formulaire (POST seulement : un GET renvoie 405 sans
// rien modifier ; le jeton CSRF est contrôlé par le middleware de la chaîne).
func (p *Pages) CreateSubmit() http.Handler {
	return websec.RequireUnsafeMethod(http.HandlerFunc(p.submit))
}

func (p *Pages) fail(w http.ResponseWriter, r *http.Request, status int, key string, f Form) {
	p.d.Render(w, r, status, TemplateCreate, Page{Lang: p.lang(r), TitleKey: "groups.create.title", ErrorKey: key, Form: f})
}

func (p *Pages) submit(w http.ResponseWriter, r *http.Request) {
	noStore(w)
	r.Body = http.MaxBytesReader(w, r.Body, maxFormBytes)
	if err := r.ParseForm(); err != nil {
		p.fail(w, r, http.StatusBadRequest, "groups.create.error.validation", Form{})
		return
	}
	f := Form{
		Name:      r.PostFormValue("name"),
		Slug:      strings.ToLower(strings.TrimSpace(r.PostFormValue("slug"))),
		Budget:    r.PostFormValue("budget"),
		RevealAt:  strings.TrimSpace(r.PostFormValue("reveal_at")),
		EventDate: strings.TrimSpace(r.PostFormValue("event_date")),
		AdminView: r.PostFormValue("admin_can_view_pair") != "",
	}
	in := groups.CreateInput{
		InstancePassword: r.PostFormValue("password"),
		ClientKey:        p.d.ClientKey(r),
		Name:             f.Name,
		Slug:             f.Slug,
		Budget:           f.Budget,
		EventDate:        f.EventDate,
		AdminCanViewPair: f.AdminView,
	}
	if f.RevealAt != "" {
		t, err := time.ParseInLocation(revealLayout, f.RevealAt, p.d.Location)
		if err != nil {
			p.fail(w, r, http.StatusUnprocessableEntity, "groups.create.error.reveal", f)
			return
		}
		in.RevealAt = &t
	}
	res, err := p.d.Groups.Create(r.Context(), in)
	if err != nil {
		status, key := errorStatus(err)
		p.fail(w, r, status, key, f)
		return
	}
	slug := res.Group.Slug
	p.d.Render(w, r, http.StatusOK, TemplateCreated, Page{
		Lang:      p.lang(r),
		TitleKey:  "groups.created.title",
		Name:      res.Group.Name,
		Slug:      slug,
		Secret:    res.AdminSecret,
		GroupURL:  p.d.PublicURL + "/" + slug,
		AdminPath: p.d.AdminPath(slug),
	})
}

// errorStatus traduit une erreur du service en statut HTTP et clé de texte :
// le message français de l'erreur n'est jamais affiché tel quel.
func errorStatus(err error) (int, string) {
	switch errs.KindOf(err) {
	case errs.KindUnauthorized:
		return http.StatusUnauthorized, "groups.create.error.password"
	case errs.KindForbidden:
		if errors.Is(err, groups.ErrTooManyAttempts) {
			return http.StatusTooManyRequests, "groups.create.error.limited"
		}
		return http.StatusForbidden, "groups.create.error.limited"
	case errs.KindValidation:
		return http.StatusUnprocessableEntity, "groups.create.error.validation"
	case errs.KindConflict:
		return http.StatusConflict, "groups.create.error.conflict"
	}
	return http.StatusInternalServerError, "core.errors.internal"
}
