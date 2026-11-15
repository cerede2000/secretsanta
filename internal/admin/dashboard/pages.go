package dashboard

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"html/template"
	"net/http"
	"strconv"
	"unicode"

	"secretsanta/internal/admin/audit"
	"secretsanta/internal/groups"
	"secretsanta/internal/i18n/negotiate"
	i18nweb "secretsanta/internal/i18n/web"
	"secretsanta/internal/participants"
	"secretsanta/internal/shared/adminnav"
	"secretsanta/internal/shared/errs"
	"secretsanta/internal/websec"
	"secretsanta/web"
)

// Groups est la part du service des groupes (F2) utilisée ici.
type Groups interface {
	BySlug(ctx context.Context, slug string) (*groups.Group, error)
}

// PageOptions rassemble les dépendances des pages.
type PageOptions struct {
	Service    *Service
	Groups     Groups
	Translator i18nweb.Translator
	// Nav donne les entrées de navigation d'administration (registre de F1b,
	// clé active en argument). Facultatif : sans lui, la navigation est vide.
	Nav func(active string) []adminnav.Item
	// NavFor est la variante qui voit la requête : les destinations du
	// registre contiennent {slug}, que seule la requête permet de remplir.
	// Elle l'emporte sur Nav quand elle est fournie.
	NavFor func(r *http.Request, active string) []adminnav.Item
	// RegistrationsPath est la page d'administration des inscriptions (F11c) ;
	// défaut : /<slug>/admin/inscriptions.
	RegistrationsPath func(slug string) string
	// ParticipantPath est la fiche d'une personne (F10.3) ; défaut :
	// /<slug>/admin/participants/<id>.
	ParticipantPath func(slug string, id int64) string
}

// Pages construit les gestionnaires du tableau de bord.
type Pages struct {
	o   PageOptions
	tpl map[string]*template.Template
}

// Noms des gabarits (un fichier par page dans web/templates/admin/dashboard).
const (
	tplDashboard = "dashboard"
	tplCheck     = "check"
	tplJournal   = "journal"
)

// Routes.
const (
	RouteDashboard = "/{slug}/admin"
	RouteCheck     = "/{slug}/admin/controle"
	RouteJournal   = "/{slug}/admin/journal"
	RouteHandover  = "/{slug}/admin/personnes/{id}/lien-remis"
	maxFormBytes   = 4 << 10
)

// Clés de texte (module admin.dashboard).
const (
	keyTitleDashboard = "admin.dashboard.title"
	keyTitleCheck     = "admin.dashboard.check.title"
	keyTitleJournal   = "admin.dashboard.journal.title"
	keyForbidden      = "admin.dashboard.error.forbidden"
	keyNotDone        = "admin.dashboard.error.not_done"
	keyNoPerson       = "admin.dashboard.error.no_person"
	keyHasEmail       = "admin.dashboard.error.has_email"
	keyInternal       = "admin.dashboard.error.internal"
	keyHandedOK       = "admin.dashboard.notice.handed_over"
	keyActionUnknown  = "admin.dashboard.action.unknown"
)

// NewPages analyse les gabarits (mise en page d'administration comprise).
func NewPages(o PageOptions) (*Pages, error) {
	if o.Service == nil || o.Groups == nil || o.Translator == nil {
		return nil, errors.New("dashboard: dépendance manquante")
	}
	if o.RegistrationsPath == nil {
		o.RegistrationsPath = func(slug string) string { return "/" + slug + "/admin/inscriptions" }
	}
	if o.ParticipantPath == nil {
		o.ParticipantPath = func(slug string, id int64) string {
			return "/" + slug + "/admin/participants/" + strconv.FormatInt(id, 10)
		}
	}
	funcs := template.FuncMap{
		"initial": initial,
	}
	for k, v := range websec.TemplateFuncsPlaceholder() {
		funcs[k] = v
	}
	for k, v := range i18nweb.FuncsPlaceholder() {
		funcs[k] = v
	}
	p := &Pages{o: o, tpl: map[string]*template.Template{}}
	for _, name := range []string{tplDashboard, tplCheck, tplJournal} {
		t, err := template.New("").Funcs(funcs).ParseFS(web.Templates,
			"templates/layout/*.html", "templates/i18n/*.html", "templates/admin/dashboard/"+name+".html")
		if err != nil {
			return nil, fmt.Errorf("dashboard: gabarit %s : %w", name, err)
		}
		p.tpl[name] = t
	}
	return p, nil
}

func initial(s string) string {
	for _, r := range s {
		return string(unicode.ToUpper(r))
	}
	return "?"
}

// personRow est une ligne du tableau de bord avec son lien vers la fiche.
type personRow struct {
	Person
	Href string
}

type journalRow struct {
	ActionKey string
	Action    string
	Person    string
	At        string
}

// pageData est la donnée commune des gabarits.
type pageData struct {
	Lang      string
	TitleKey  string
	Nav       []adminnav.Item
	Slug      string
	GroupName string

	View              *View
	People            []personRow
	NoEmail           []personRow
	RegistrationsHref string
	Check             *CheckResult
	Journal           []journalRow
	ErrorKey          string
	NoticeKey         string
}

func (p *Pages) render(w http.ResponseWriter, r *http.Request, status int, name, active string, d pageData) {
	clone, err := p.tpl[name].Clone()
	if err != nil {
		http.Error(w, "Internal error", http.StatusInternalServerError)
		return
	}
	clone.Funcs(websec.TemplateFuncs(r)).Funcs(i18nweb.Funcs(p.o.Translator, r))
	d.Lang = negotiate.Lang(r.Context(), p.o.Translator.DefaultLang())
	if p.o.NavFor != nil {
		d.Nav = p.o.NavFor(r, active)
	} else if p.o.Nav != nil {
		d.Nav = p.o.Nav(active)
	}
	var buf bytes.Buffer
	if err := clone.ExecuteTemplate(&buf, "layout/admin", d); err != nil {
		http.Error(w, "Internal error", http.StatusInternalServerError)
		return
	}
	h := w.Header()
	h.Set("Content-Type", "text/html; charset=utf-8")
	h.Set("Cache-Control", "no-store")
	h.Set("Referrer-Policy", "no-referrer")
	w.WriteHeader(status)
	_, _ = w.Write(buf.Bytes())
}

func notFound(w http.ResponseWriter) {
	http.Error(w, "404 page not found", http.StatusNotFound)
}

// admin retrouve le groupe du chemin et exige la session administrateur de ce
// groupe : la session d'un autre groupe ne vaut rien ici. ok est faux quand la
// réponse a déjà été écrite.
func (p *Pages) admin(w http.ResponseWriter, r *http.Request) (g *groups.Group, a participants.Actor, ok bool) {
	slug := r.PathValue("slug")
	g, err := p.o.Groups.BySlug(r.Context(), slug)
	if err != nil || g == nil {
		notFound(w)
		return nil, a, false
	}
	if _, has := websec.AdminSession(r.Context(), strconv.FormatInt(g.ID, 10)); !has {
		if r.Method == http.MethodGet {
			http.Redirect(w, r, "/"+g.Slug+"/admin/connexion", http.StatusSeeOther)
		} else {
			http.Error(w, "Accès refusé.", http.StatusForbidden)
		}
		return nil, a, false
	}
	return g, participants.AdminActor(g.ID), true
}

func (p *Pages) base(g *groups.Group) pageData {
	return pageData{Slug: g.Slug, GroupName: g.Name, RegistrationsHref: p.o.RegistrationsPath(g.Slug)}
}

func (p *Pages) rows(slug string, in []Person) []personRow {
	out := make([]personRow, 0, len(in))
	for _, pe := range in {
		out = append(out, personRow{Person: pe, Href: p.o.ParticipantPath(slug, pe.ID)})
	}
	return out
}

// errorKey traduit une erreur du service en clé de texte et statut HTTP.
func errorKey(err error) (string, int) {
	switch errs.KindOf(err) {
	case errs.KindForbidden, errs.KindUnauthorized:
		return keyForbidden, http.StatusForbidden
	case errs.KindNotFound:
		return keyNoPerson, http.StatusNotFound
	case errs.KindConflict:
		return keyNotDone, http.StatusConflict
	case errs.KindValidation:
		return keyHasEmail, http.StatusUnprocessableEntity
	}
	return keyInternal, http.StatusInternalServerError
}

func (p *Pages) showDashboard(w http.ResponseWriter, r *http.Request, g *groups.Group, a participants.Actor, status int, errKey, notice string) {
	d := p.base(g)
	d.TitleKey = keyTitleDashboard
	v, err := p.o.Service.Dashboard(r.Context(), a, g.ID)
	if err != nil {
		key, st := errorKey(err)
		d.ErrorKey = key
		p.render(w, r, st, tplDashboard, "dashboard", d)
		return
	}
	d.View = v
	d.People = p.rows(g.Slug, v.People)
	for _, row := range d.People {
		if !row.HasEmail {
			d.NoEmail = append(d.NoEmail, row)
		}
	}
	d.ErrorKey, d.NoticeKey = errKey, notice
	p.render(w, r, status, tplDashboard, "dashboard", d)
}

// DashboardPage rend screen-admin-tableau (GET /{slug}/admin).
func (p *Pages) DashboardPage() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		g, a, ok := p.admin(w, r)
		if !ok {
			return
		}
		notice := ""
		if r.URL.Query().Get("remis") == "1" {
			notice = keyHandedOK
		}
		p.showDashboard(w, r, g, a, http.StatusOK, "", notice)
	})
}

// CheckPage rend le rapport du contrôle du tirage (GET /{slug}/admin/controle) :
// il ne modifie rien et ne contient aucun nom.
func (p *Pages) CheckPage() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		g, a, ok := p.admin(w, r)
		if !ok {
			return
		}
		d := p.base(g)
		d.TitleKey = keyTitleCheck
		res, err := p.o.Service.CheckDraw(r.Context(), a, g.ID)
		if err != nil {
			key, st := errorKey(err)
			d.ErrorKey = key
			p.render(w, r, st, tplCheck, "dashboard", d)
			return
		}
		d.Check = res
		p.render(w, r, http.StatusOK, tplCheck, "dashboard", d)
	})
}

// JournalPage rend le journal des actions sensibles (GET /{slug}/admin/journal).
func (p *Pages) JournalPage() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		g, a, ok := p.admin(w, r)
		if !ok {
			return
		}
		d := p.base(g)
		d.TitleKey = keyTitleJournal
		entries, err := p.o.Service.o.Audit.List(r.Context(), a, g.ID, audit.MaxList)
		if err == nil {
			var list []participants.Participant
			list, err = p.o.Service.o.Participants.List(r.Context(), a, g.ID)
			names := map[int64]string{}
			for _, pe := range list {
				names[pe.ID] = pe.FirstName + " " + pe.Name
			}
			lang := negotiate.Lang(r.Context(), p.o.Translator.DefaultLang())
			for _, e := range entries {
				key := "admin.dashboard.action." + e.Action
				if p.o.Translator.T(lang, key, nil) == key {
					key = keyActionUnknown // code d'une autre fonctionnalité sans texte ici
				}
				d.Journal = append(d.Journal, journalRow{
					ActionKey: key, Action: e.Action,
					Person: names[e.ParticipantID], At: e.At.Format("2006-01-02 15:04"),
				})
			}
		}
		if err != nil {
			key, st := errorKey(err)
			d.ErrorKey = key
			p.render(w, r, st, tplJournal, "dashboard", d)
			return
		}
		p.render(w, r, http.StatusOK, tplJournal, "dashboard", d)
	})
}

// Handover consigne qu'un lien a été remis (POST
// /{slug}/admin/personnes/{id}/lien-remis).
func (p *Pages) Handover() http.Handler {
	return websec.RequireUnsafeMethod(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		g, a, ok := p.admin(w, r)
		if !ok {
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, maxFormBytes)
		id, convErr := strconv.ParseInt(r.PathValue("id"), 10, 64)
		if convErr != nil || id <= 0 {
			p.showDashboard(w, r, g, a, http.StatusNotFound, keyNoPerson, "")
			return
		}
		if err := p.o.Service.MarkLinkHandedOver(r.Context(), a, g.ID, id); err != nil {
			key, st := errorKey(err)
			p.showDashboard(w, r, g, a, st, key, "")
			return
		}
		http.Redirect(w, r, "/"+g.Slug+"/admin?remis=1", http.StatusSeeOther)
	}))
}

// Route est une route et son gestionnaire.
type Route struct {
	Pattern string
	Handler http.Handler
	// Modifies est vrai pour les routes qui changent l'état (déjà enveloppées
	// par websec.RequireUnsafeMethod).
	Modifies bool
}

// Routes liste les routes à brancher par l'orchestrateur. Elles attendent les
// middlewares SecurityHeaders, CSRF et Sessions (l'espace d'administration
// lit websec.AdminSession).
func (p *Pages) Routes() []Route {
	return []Route{
		{"GET " + RouteDashboard, p.DashboardPage(), false},
		{"GET " + RouteCheck, p.CheckPage(), false},
		{"GET " + RouteJournal, p.JournalPage(), false},
		{"POST " + RouteHandover, p.Handover(), true},
	}
}

// Register branche les routes sur mux.
func (p *Pages) Register(mux *http.ServeMux) {
	for _, r := range p.Routes() {
		mux.Handle(r.Pattern, r.Handler)
	}
}
