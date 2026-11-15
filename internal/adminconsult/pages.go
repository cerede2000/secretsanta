package adminconsult

import (
	"bytes"
	"context"
	"errors"
	"html/template"
	"net/http"
	"strconv"
	"strings"
	"time"

	"secretsanta/internal/access/limiter"
	"secretsanta/internal/groups"
	"secretsanta/internal/i18n/negotiate"
	i18nweb "secretsanta/internal/i18n/web"
	"secretsanta/internal/participants"
	"secretsanta/internal/shared/adminnav"
	"secretsanta/internal/shared/errs"
	"secretsanta/internal/websec"
	"secretsanta/web"
)

// Routes.
const (
	// RouteConsult : formulaire (GET) et consultation (POST). Elle n'est
	// servie que si l'option du groupe est active ; sinon la réponse est
	// celle d'une route inexistante.
	RouteConsult = "/{slug}/admin/consultation"
	// RouteJournal : journal des consultations, visible de tout membre
	// connecté du groupe (participant ou administrateur).
	RouteJournal = "/{slug}/consultations"

	maxFormBytes   = 4 << 10
	maxPasswordLen = 512
	navKey         = "plus"
	dateLayout     = "2006-01-02"
)

// Clés de texte (module adminconsult).
const (
	keyTitle        = "adminconsult.title"
	keyJournalTitle = "adminconsult.journal.title"
	keyBadPassword  = "adminconsult.error.password"
	keyNoPassword   = "adminconsult.error.password_required"
	keyChoose       = "adminconsult.error.person"
	keyNoDraw       = "adminconsult.error.no_draw"
	keyLimited      = "adminconsult.error.limited"
	keyInternal     = "adminconsult.error.internal"
	keyForbidden    = "adminconsult.error.forbidden"
	keyInvalid      = "adminconsult.error.invalid"
)

// PageGroups est la part du service des groupes utilisée par les pages.
type PageGroups interface {
	BySlug(ctx context.Context, slug string) (*groups.Group, error)
}

// PageOptions rassemble les dépendances des pages.
type PageOptions struct {
	Service    *Service
	Groups     PageGroups
	Translator i18nweb.Translator
	// Location est le fuseau d'affichage des dates (défaut UTC).
	Location *time.Location
	// NavFor donne les entrées de navigation d'administration ; facultatif.
	NavFor func(r *http.Request, active string) []adminnav.Item
}

// Pages construit les gestionnaires.
type Pages struct {
	o   PageOptions
	tpl *template.Template
}

// NewPages analyse les gabarits.
func NewPages(o PageOptions) (*Pages, error) {
	if o.Service == nil || o.Groups == nil || o.Translator == nil {
		return nil, errors.New("adminconsult: dépendance manquante")
	}
	if o.Location == nil {
		o.Location = time.UTC
	}
	funcs := template.FuncMap{}
	for k, v := range websec.TemplateFuncsPlaceholder() {
		funcs[k] = v
	}
	for k, v := range i18nweb.FuncsPlaceholder() {
		funcs[k] = v
	}
	t, err := template.New("").Funcs(funcs).ParseFS(web.Templates,
		"templates/layout/*.html", "templates/i18n/*.html", "templates/adminconsult/*.html")
	if err != nil {
		return nil, errors.New("adminconsult: gabarit : " + err.Error())
	}
	return &Pages{o: o, tpl: t}, nil
}

type pageData struct {
	Lang     string
	TitleKey string
	Nav      []adminnav.Item
	Slug     string
	// Mode : "form", "result" ou "journal".
	Mode string

	// Formulaire.
	People   []Person
	Selected int64
	// Résultat d'une consultation (une seule personne).
	Result *Consultation

	// Journal.
	Journal []string
	Admin   bool

	ErrorKey      string // erreur générale
	PasswordError string // erreur sous le champ du mot de passe
	PersonError   string // erreur sous le choix de la personne
}

func (p *Pages) render(w http.ResponseWriter, r *http.Request, status int, layout string, d pageData) {
	clone, err := p.tpl.Clone()
	if err != nil {
		http.Error(w, "Internal error", http.StatusInternalServerError)
		return
	}
	clone.Funcs(websec.TemplateFuncs(r)).Funcs(i18nweb.Funcs(p.o.Translator, r))
	d.Lang = negotiate.Lang(r.Context(), p.o.Translator.DefaultLang())
	if layout == "layout/admin" && p.o.NavFor != nil {
		d.Nav = p.o.NavFor(r, navKey)
	}
	var buf bytes.Buffer
	if err := clone.ExecuteTemplate(&buf, layout, d); err != nil {
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

// hasCode dit si err porte le code donné.
func hasCode(err error, code string) bool {
	c, _ := errs.CodeOf(err)
	return c == code
}

// notServed répond exactement comme une route que le routeur ne connaît pas.
func notServed(w http.ResponseWriter, r *http.Request) { http.NotFound(w, r) }

// consultGroup résout le groupe de la route de consultation. Un groupe
// inconnu ou dont l'option est désactivée reçoit la réponse d'une route
// inexistante, avant toute autre vérification.
func (p *Pages) consultGroup(w http.ResponseWriter, r *http.Request) (*groups.Group, participants.Actor, bool) {
	var a participants.Actor
	g, err := p.o.Groups.BySlug(r.Context(), r.PathValue("slug"))
	if err != nil || g == nil || !g.AdminCanViewPair {
		notServed(w, r)
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

// form rend le formulaire de consultation, avec ses erreurs éventuelles.
func (p *Pages) form(w http.ResponseWriter, r *http.Request, g *groups.Group, a participants.Actor, status int, d pageData) {
	people, err := p.o.Service.People(r.Context(), a, g.ID)
	if err != nil {
		if hasCode(err, CodeDisabled) {
			notServed(w, r)
			return
		}
		d.ErrorKey = keyInternal
		status = http.StatusInternalServerError
	}
	d.Slug, d.People, d.TitleKey, d.Mode = g.Slug, people, keyTitle, "form"
	p.render(w, r, status, "layout/admin", d)
}

// ConsultPage rend le formulaire (GET /{slug}/admin/consultation) : choix
// d'UNE personne et ressaisie du mot de passe. Elle ne modifie rien.
func (p *Pages) ConsultPage() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		g, a, ok := p.consultGroup(w, r)
		if !ok {
			return
		}
		p.form(w, r, g, a, http.StatusOK, pageData{})
	})
}

// Consult consulte la paire d'une personne (POST /{slug}/admin/consultation)
// et rend le résultat directement, sans adresse qui le rappellerait. La
// consultation est consignée au journal des participants.
func (p *Pages) Consult() http.Handler {
	return websec.RequireUnsafeMethod(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		g, a, ok := p.consultGroup(w, r)
		if !ok {
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, maxFormBytes)
		if err := r.ParseForm(); err != nil {
			p.form(w, r, g, a, http.StatusBadRequest, pageData{ErrorKey: keyInvalid})
			return
		}
		// Une seule personne : un champ « person » répété est refusé.
		vals := r.PostForm["person"]
		var person int64
		if len(vals) == 1 {
			person, _ = strconv.ParseInt(strings.TrimSpace(vals[0]), 10, 64)
		}
		d := pageData{Selected: person}
		if person <= 0 {
			d.PersonError = keyChoose
		}
		password := r.PostForm.Get("password")
		if password == "" || len(password) > maxPasswordLen {
			d.PasswordError = keyNoPassword
		}
		if d.PersonError != "" || d.PasswordError != "" {
			p.form(w, r, g, a, http.StatusUnprocessableEntity, d)
			return
		}
		res, err := p.o.Service.Consult(r.Context(), a, g.ID, person, password)
		if err != nil {
			status := http.StatusInternalServerError
			switch {
			case hasCode(err, CodeDisabled):
				notServed(w, r)
				return
			case errors.Is(err, limiter.ErrLimited):
				d.ErrorKey, status = keyLimited, http.StatusTooManyRequests
			case hasCode(err, CodeBadPassword):
				d.PasswordError, status = keyBadPassword, http.StatusForbidden
			case hasCode(err, CodeNoDraw):
				d.ErrorKey, status = keyNoDraw, http.StatusConflict
			case errs.KindOf(err) == errs.KindNotFound:
				d.PersonError, status = keyChoose, http.StatusUnprocessableEntity
			case errs.KindOf(err) == errs.KindForbidden || errs.KindOf(err) == errs.KindUnauthorized:
				d.ErrorKey, status = keyForbidden, http.StatusForbidden
			default:
				d.ErrorKey = keyInternal
			}
			p.form(w, r, g, a, status, d)
			return
		}
		p.render(w, r, http.StatusOK, "layout/admin", pageData{Slug: g.Slug, TitleKey: keyTitle, Mode: "result", Result: res})
	}))
}

// JournalPage rend le journal des consultations (GET /{slug}/consultations)
// pour tout membre connecté du groupe : des dates, aucun nom.
func (p *Pages) JournalPage() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		g, err := p.o.Groups.BySlug(r.Context(), r.PathValue("slug"))
		if err != nil || g == nil {
			notServed(w, r)
			return
		}
		gid := strconv.FormatInt(g.ID, 10)
		var a participants.Actor
		admin := false
		if sess, ok := websec.ParticipantSession(r.Context(), gid); ok {
			pid, perr := strconv.ParseInt(sess.Subject, 10, 64)
			if perr == nil && pid > 0 {
				a = participants.ParticipantActor(g.ID, pid)
			}
		}
		if a.Kind == participants.ActorAnonymous {
			if _, ok := websec.AdminSession(r.Context(), gid); ok {
				a, admin = participants.AdminActor(g.ID), true
			}
		}
		if a.Kind == participants.ActorAnonymous {
			http.Redirect(w, r, "/"+g.Slug+"/connexion", http.StatusSeeOther)
			return
		}
		entries, err := p.o.Service.Journal(r.Context(), a, g.ID)
		d := pageData{Slug: g.Slug, TitleKey: keyJournalTitle, Mode: "journal", Admin: admin}
		status := http.StatusOK
		if err != nil {
			d.ErrorKey, status = keyInternal, http.StatusInternalServerError
		}
		for _, e := range entries {
			d.Journal = append(d.Journal, e.At.In(p.o.Location).Format(dateLayout))
		}
		layout := "layout/base"
		if admin {
			layout = "layout/admin"
		}
		p.render(w, r, status, layout, d)
	})
}

// Route est une route et son gestionnaire.
type Route struct {
	Pattern string
	Handler http.Handler
	// Modifies est vrai pour les routes qui changent l'état (déjà enveloppées
	// par websec.RequireUnsafeMethod).
	Modifies bool
}

// Routes liste les routes à brancher par l'orchestrateur ; elles attendent les
// middlewares SecurityHeaders, CSRF et Sessions.
func (p *Pages) Routes() []Route {
	return []Route{
		{"GET " + RouteConsult, p.ConsultPage(), false},
		{"POST " + RouteConsult, p.Consult(), true},
		{"GET " + RouteJournal, p.JournalPage(), false},
	}
}

// Register branche les routes sur mux.
func (p *Pages) Register(mux *http.ServeMux) {
	for _, r := range p.Routes() {
		mux.Handle(r.Pattern, r.Handler)
	}
}
