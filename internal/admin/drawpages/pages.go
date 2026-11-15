// Package drawpages assemble la page d'administration du tirage (F10.4,
// screen-admin-tirage) en trois onglets : Tirage, Ajuster, Historique.
//
// Elle s'appuie sur les services de F4 (draw) et de F4d (reveal) : état du
// tirage, date de révélation, lancement avec confirmation, rapport de
// contrôle, « ouvrir maintenant », choix sur les inscriptions en attente,
// récapitulatif des rôles et de la capacité. Aucune paire n'est jamais lue ni
// affichée : seuls des comptes et des faits traversent ce paquet.
//
// L'onglet Ajuster renvoie à la page « Ajuster le tirage » (F10c) et le
// panneau de modification des rôles en lot est celui de F10.6 (paquet roles).
package drawpages

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"html/template"
	"net/http"
	"strconv"
	"time"

	"secretsanta/internal/draw"
	"secretsanta/internal/groups"
	"secretsanta/internal/i18n/negotiate"
	i18nweb "secretsanta/internal/i18n/web"
	"secretsanta/internal/participants"
	"secretsanta/internal/reveal"
	"secretsanta/internal/shared/adminnav"
	"secretsanta/internal/shared/errs"
	"secretsanta/internal/shared/ports"
	"secretsanta/internal/websec"
	"secretsanta/web"
)

// Groups est la part du service des groupes utilisée ici.
type Groups interface {
	BySlug(ctx context.Context, slug string) (*groups.Group, error)
}

// Draws est la part du service de tirage (F4) utilisée ici. Aucune méthode ne
// renvoie de paire.
type Draws interface {
	Status(ctx context.Context, groupID int64) (draw.Status, error)
	Check(ctx context.Context, a participants.Actor, groupID int64, capConfirmed bool) (*draw.CheckReport, error)
	Report(ctx context.Context, a participants.Actor, groupID int64) (*draw.CheckReport, error)
	Draw(ctx context.Context, a participants.Actor, groupID int64, req draw.Request) (*draw.Outcome, error)
	Redraw(ctx context.Context, a participants.Actor, groupID int64, req draw.Request) (*draw.Outcome, error)
	PendingRegistrations(ctx context.Context, a participants.Actor, groupID int64) (int, error)
	ResolvePending(ctx context.Context, a participants.Actor, groupID int64, how ports.RegistrationResolution) error
	History(ctx context.Context, a participants.Actor, groupID int64) ([]draw.HistoryEntry, error)
}

// Reveal est la part du service de révélation (F4d) utilisée ici.
type Reveal interface {
	Facts(ctx context.Context, a participants.Actor, groupID int64) (*reveal.Facts, error)
	SetRevealDate(ctx context.Context, a participants.Actor, groupID int64, at *time.Time) error
	OpenNow(ctx context.Context, a participants.Actor, groupID int64, confirmed bool) (bool, error)
}

// Auditor consigne une action sensible (journal de F10.1, *audit.Service).
type Auditor interface {
	Record(ctx context.Context, a participants.Actor, groupID int64, action string, participantID int64) error
}

// Options rassemble les dépendances.
type Options struct {
	Groups     Groups
	Draws      Draws
	Reveal     Reveal
	Translator i18nweb.Translator
	// Audit est facultatif ; sans lui, rien n'est consigné.
	Audit Auditor
	// Location est le fuseau d'affichage et de saisie des dates (défaut UTC).
	Location *time.Location
	// OnDrawChanged est appelé après un tirage (par exemple
	// reveal.Service.OnDrawChanged) ; facultatif.
	OnDrawChanged func(ctx context.Context, groupID int64) error
	// NavFor donne les entrées de navigation d'administration ; facultatif.
	NavFor func(r *http.Request, active string) []adminnav.Item
	// AdjustPath (page F10c) et RolesPath (panneau F10.6) ; défauts
	// /<slug>/admin/ajuster et /<slug>/admin/roles.
	AdjustPath func(slug string) string
	RolesPath  func(slug string) string
}

// Pages construit les gestionnaires.
type Pages struct {
	o   Options
	tpl *template.Template
}

// Routes.
const (
	RouteDraw   = "/{slug}/admin/tirage"
	RouteLaunch = "/{slug}/admin/tirage/lancer"
	RouteDate   = "/{slug}/admin/tirage/date"
	RouteOpen   = "/{slug}/admin/tirage/ouvrir"

	maxFormBytes = 4 << 10
	navKey       = "tirage"
	dateLayout   = "2006-01-02T15:04"
)

// Actions consignées dans le journal.
const (
	ActionLaunch = "tirage_lance"
	ActionRedraw = "tirage_refait"
	ActionOpen   = "revelation_ouverte"
	ActionDate   = "date_revelation"
)

// Onglets.
const (
	tabDraw    = "tirage"
	tabAdjust  = "ajuster"
	tabHistory = "historique"
)

// Clés de texte (module admin.draw).
const (
	keyTitle       = "admin.draw.title"
	keyForbidden   = "admin.draw.error.forbidden"
	keyInternal    = "admin.draw.error.internal"
	keyConflict    = "admin.draw.error.conflict"
	keyInvalid     = "admin.draw.error.invalid"
	keyConfirm     = "admin.draw.error.confirm"
	keyChoose      = "admin.draw.error.choose_pending"
	keyPending     = "admin.draw.error.pending"
	keyInfeasible  = "admin.draw.error.infeasible"
	keyDateInvalid = "admin.draw.error.date_invalid"
	keyDateLocked  = "admin.draw.error.date_locked"
	keyNotDone     = "admin.draw.error.not_done"
	keyDone        = "admin.draw.notice.done"
	keyRedone      = "admin.draw.notice.redone"
	keyOpened      = "admin.draw.notice.opened"
	keyDateSaved   = "admin.draw.notice.date_saved"
)

// NewPages analyse les gabarits.
func NewPages(o Options) (*Pages, error) {
	if o.Groups == nil || o.Draws == nil || o.Reveal == nil || o.Translator == nil {
		return nil, errors.New("drawpages: dépendance manquante")
	}
	if o.Location == nil {
		o.Location = time.UTC
	}
	if o.AdjustPath == nil {
		o.AdjustPath = func(slug string) string { return "/" + slug + "/admin/ajuster" }
	}
	if o.RolesPath == nil {
		o.RolesPath = func(slug string) string { return "/" + slug + "/admin/roles" }
	}
	funcs := template.FuncMap{}
	for k, v := range websec.TemplateFuncsPlaceholder() {
		funcs[k] = v
	}
	for k, v := range i18nweb.FuncsPlaceholder() {
		funcs[k] = v
	}
	t, err := template.New("").Funcs(funcs).ParseFS(web.Templates,
		"templates/layout/*.html", "templates/i18n/*.html", "templates/admin/draw/*.html")
	if err != nil {
		return nil, fmt.Errorf("drawpages: gabarit : %w", err)
	}
	return &Pages{o: o, tpl: t}, nil
}

// capacity est le jugement de capacité, en comptes.
type capacity struct {
	Short          bool
	Cause          string // suffixe de clé : too_many_one_gift, cap_too_low, too_few_givers, other
	Missing        int
	NeedCapConfirm bool
}

type historyRow struct {
	Year      int
	Current   bool
	KindKey   string
	Count     int
	DrawnAt   string
	Revisions int
}

type pageData struct {
	Lang      string
	TitleKey  string
	Nav       []adminnav.Item
	Slug      string
	GroupName string
	Tab       string

	Done        bool
	DrawnAt     string
	Open        bool
	DateLocked  bool
	RevealValue string
	RevealShown string
	Targets     int

	Pending   int
	Report    *draw.CheckReport
	Cap       capacity
	CanLaunch bool

	Confirm string // "", "lancer" ou "ouvrir"

	// Valeurs de formulaire du choix sur les inscriptions en attente.
	IgnoreValue string
	DeleteValue string

	History []historyRow

	AdjustHref string
	RolesHref  string
	CheckHref  string

	ErrorKey  string
	NoticeKey string
}

func (p *Pages) render(w http.ResponseWriter, r *http.Request, status int, d pageData) {
	clone, err := p.tpl.Clone()
	if err != nil {
		http.Error(w, "Internal error", http.StatusInternalServerError)
		return
	}
	clone.Funcs(websec.TemplateFuncs(r)).Funcs(i18nweb.Funcs(p.o.Translator, r))
	d.Lang = negotiate.Lang(r.Context(), p.o.Translator.DefaultLang())
	d.TitleKey = keyTitle
	if p.o.NavFor != nil {
		d.Nav = p.o.NavFor(r, navKey)
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

func (p *Pages) admin(w http.ResponseWriter, r *http.Request) (*groups.Group, participants.Actor, bool) {
	var a participants.Actor
	g, err := p.o.Groups.BySlug(r.Context(), r.PathValue("slug"))
	if err != nil || g == nil {
		http.Error(w, "404 page not found", http.StatusNotFound)
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

// errorKey traduit une erreur de service en clé de texte et statut HTTP.
func errorKey(err error) (string, int) {
	if code, _ := errs.CodeOf(err); code != "" {
		switch code {
		case draw.CodePendingRegistrations:
			return keyPending, http.StatusConflict
		case draw.CodeInfeasible:
			return keyInfeasible, http.StatusConflict
		case reveal.CodeDateLocked:
			return keyDateLocked, http.StatusConflict
		}
	}
	switch errs.KindOf(err) {
	case errs.KindForbidden, errs.KindUnauthorized:
		return keyForbidden, http.StatusForbidden
	case errs.KindConflict:
		return keyConflict, http.StatusConflict
	case errs.KindValidation:
		return keyInvalid, http.StatusUnprocessableEntity
	case errs.KindNotFound:
		return keyNotDone, http.StatusNotFound
	}
	return keyInternal, http.StatusInternalServerError
}

var causeSuffix = map[string]string{
	"trop_de_personnes_a_un_cadeau": "too_many_one_gift",
	"plafond_trop_bas":              "cap_too_low",
	"trop_peu_de_tireurs":           "too_few_givers",
}

// judge lit le rapport de capacité : le moteur fait foi, son explication est
// reprise par cause et par comptes, jamais par son texte.
func judge(err error) capacity {
	if err == nil {
		return capacity{}
	}
	code, params := errs.CodeOf(err)
	if code != draw.CodeInfeasible {
		return capacity{Short: true, Cause: "other"}
	}
	cause, _ := params["cause"].(string)
	if cause == "plafond_a_confirmer" {
		return capacity{NeedCapConfirm: true}
	}
	c := capacity{Short: true, Cause: "other"}
	if s, ok := causeSuffix[cause]; ok {
		c.Cause = s
	}
	switch m := params["missing"].(type) {
	case int:
		c.Missing = m
	case int64:
		c.Missing = int(m)
	}
	return c
}

// view lit l'état et prépare la page. Une lecture qui échoue est renvoyée.
func (p *Pages) view(ctx context.Context, g *groups.Group, a participants.Actor, tab string) (*pageData, error) {
	d := &pageData{IgnoreValue: string(ports.ResolveIgnore), DeleteValue: string(ports.ResolveDelete), Slug: g.Slug, GroupName: g.Name, Tab: tab,
		AdjustHref: p.o.AdjustPath(g.Slug), RolesHref: p.o.RolesPath(g.Slug), CheckHref: "/" + g.Slug + "/admin/controle"}
	st, err := p.o.Draws.Status(ctx, g.ID)
	if err != nil {
		return nil, err
	}
	d.Done = st.Done
	if st.Done {
		d.DrawnAt = st.DrawnAt.In(p.o.Location).Format("2006-01-02 15:04")
	}
	f, err := p.o.Reveal.Facts(ctx, a, g.ID)
	if err != nil {
		return nil, err
	}
	d.Open, d.DateLocked, d.Targets = f.Open, f.DateLocked, f.Targets
	if f.RevealAt != nil {
		t := f.RevealAt.In(p.o.Location)
		d.RevealValue, d.RevealShown = t.Format(dateLayout), t.Format("2006-01-02 15:04")
	}
	if d.Pending, err = p.o.Draws.PendingRegistrations(ctx, a, g.ID); err != nil {
		return nil, err
	}
	rep, cerr := p.o.Draws.Check(ctx, a, g.ID, false)
	d.Cap = judge(cerr)
	if cerr != nil && !d.Cap.Short && !d.Cap.NeedCapConfirm {
		return nil, cerr
	}
	d.Report = rep
	if st.Done {
		if full, err := p.o.Draws.Report(ctx, a, g.ID); err == nil {
			d.Report = full
		}
	}
	d.CanLaunch = !d.Cap.Short
	if tab == tabHistory {
		hist, err := p.o.Draws.History(ctx, a, g.ID)
		if err != nil {
			return nil, err
		}
		for _, h := range hist {
			d.History = append(d.History, historyRow{Year: h.Year, Current: !h.Archived,
				KindKey: "admin.draw.history.kind." + h.Kind, Count: h.PairCount,
				DrawnAt: h.DrawnAt.In(p.o.Location).Format("2006-01-02 15:04"), Revisions: h.Revision})
		}
	}
	return d, nil
}

func tabOf(r *http.Request) string {
	switch r.URL.Query().Get("onglet") {
	case tabAdjust:
		return tabAdjust
	case tabHistory:
		return tabHistory
	}
	return tabDraw
}

// show lit l'état puis rend la page ; en cas d'erreur de lecture, la page
// porte le message.
func (p *Pages) show(w http.ResponseWriter, r *http.Request, g *groups.Group, a participants.Actor, status int, tab, confirm, errKey, notice string) {
	d, err := p.view(r.Context(), g, a, tab)
	if err != nil {
		key, st := errorKey(err)
		p.render(w, r, st, pageData{Slug: g.Slug, GroupName: g.Name, Tab: tab, ErrorKey: key})
		return
	}
	d.Confirm, d.ErrorKey, d.NoticeKey = confirm, errKey, notice
	p.render(w, r, status, *d)
}

// DrawPage rend screen-admin-tirage (GET /{slug}/admin/tirage). Elle ne
// modifie rien ; « confirmer=lancer|ouvrir » ouvre le volet de confirmation.
func (p *Pages) DrawPage() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		g, a, ok := p.admin(w, r)
		if !ok {
			return
		}
		confirm := ""
		switch r.URL.Query().Get("confirmer") {
		case "lancer":
			confirm = "lancer"
		case "ouvrir":
			confirm = "ouvrir"
		}
		notice := ""
		switch r.URL.Query().Get("ok") {
		case "lance":
			notice = keyDone
		case "refait":
			notice = keyRedone
		case "ouvert":
			notice = keyOpened
		case "date":
			notice = keyDateSaved
		}
		p.show(w, r, g, a, http.StatusOK, tabOf(r), confirm, "", notice)
	})
}

func (p *Pages) record(ctx context.Context, a participants.Actor, groupID int64, action string) {
	if p.o.Audit != nil {
		_ = p.o.Audit.Record(ctx, a, groupID, action, 0)
	}
}

// Launch lance le tirage, ou le refait en entier (POST /{slug}/admin/tirage/lancer).
// La confirmation est exigée ; avec des inscriptions en attente, le choix
// « ignorer » ou « supprimer » l'est aussi, avant tout envoi.
func (p *Pages) Launch() http.Handler {
	return websec.RequireUnsafeMethod(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		g, a, ok := p.admin(w, r)
		if !ok {
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, maxFormBytes)
		if err := r.ParseForm(); err != nil {
			p.show(w, r, g, a, http.StatusBadRequest, tabDraw, "lancer", keyInvalid, "")
			return
		}
		ctx := r.Context()
		if r.PostForm.Get("confirm") != "1" {
			p.show(w, r, g, a, http.StatusUnprocessableEntity, tabDraw, "lancer", keyConfirm, "")
			return
		}
		pending, err := p.o.Draws.PendingRegistrations(ctx, a, g.ID)
		if err != nil {
			key, st := errorKey(err)
			p.show(w, r, g, a, st, tabDraw, "lancer", key, "")
			return
		}
		if pending > 0 {
			how := ports.RegistrationResolution(r.PostForm.Get("pending"))
			if how != ports.ResolveIgnore && how != ports.ResolveDelete {
				p.show(w, r, g, a, http.StatusUnprocessableEntity, tabDraw, "lancer", keyChoose, "")
				return
			}
			// Le tirage est refusé plus loin si le plafond ou les rôles ne
			// le permettent pas : on ne touche pas aux inscriptions avant
			// d'avoir vérifié que le lancement peut avoir lieu.
			if _, cerr := p.o.Draws.Check(ctx, a, g.ID, r.PostForm.Get("cap_confirmed") == "1"); cerr != nil {
				key, st := errorKey(cerr)
				p.show(w, r, g, a, st, tabDraw, "lancer", key, "")
				return
			}
			if err := p.o.Draws.ResolvePending(ctx, a, g.ID, how); err != nil {
				key, st := errorKey(err)
				p.show(w, r, g, a, st, tabDraw, "lancer", key, "")
				return
			}
		}
		st, err := p.o.Draws.Status(ctx, g.ID)
		if err != nil {
			key, code := errorKey(err)
			p.show(w, r, g, a, code, tabDraw, "lancer", key, "")
			return
		}
		req := draw.Request{Confirmed: true, CapConfirmed: r.PostForm.Get("cap_confirmed") == "1"}
		action, ok2 := ActionLaunch, "lance"
		if st.Done {
			_, err = p.o.Draws.Redraw(ctx, a, g.ID, req)
			action, ok2 = ActionRedraw, "refait"
		} else {
			_, err = p.o.Draws.Draw(ctx, a, g.ID, req)
		}
		if err != nil {
			key, code := errorKey(err)
			p.show(w, r, g, a, code, tabDraw, "lancer", key, "")
			return
		}
		p.record(ctx, a, g.ID, action)
		if p.o.OnDrawChanged != nil {
			_ = p.o.OnDrawChanged(ctx, g.ID)
		}
		http.Redirect(w, r, "/"+g.Slug+"/admin/tirage?ok="+ok2, http.StatusSeeOther)
	}))
}

// SetDate modifie la date de révélation (POST /{slug}/admin/tirage/date).
func (p *Pages) SetDate() http.Handler {
	return websec.RequireUnsafeMethod(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		g, a, ok := p.admin(w, r)
		if !ok {
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, maxFormBytes)
		if err := r.ParseForm(); err != nil {
			p.show(w, r, g, a, http.StatusBadRequest, tabDraw, "", keyInvalid, "")
			return
		}
		var at *time.Time
		if raw := r.PostForm.Get("date"); raw != "" {
			t, err := time.ParseInLocation(dateLayout, raw, p.o.Location)
			if err != nil {
				p.show(w, r, g, a, http.StatusUnprocessableEntity, tabDraw, "", keyDateInvalid, "")
				return
			}
			at = &t
		}
		if err := p.o.Reveal.SetRevealDate(r.Context(), a, g.ID, at); err != nil {
			key, st := errorKey(err)
			if st == http.StatusUnprocessableEntity {
				key = keyDateInvalid
			}
			p.show(w, r, g, a, st, tabDraw, "", key, "")
			return
		}
		p.record(r.Context(), a, g.ID, ActionDate)
		http.Redirect(w, r, "/"+g.Slug+"/admin/tirage?ok=date", http.StatusSeeOther)
	}))
}

// OpenNow ouvre la révélation tout de suite (POST /{slug}/admin/tirage/ouvrir).
func (p *Pages) OpenNow() http.Handler {
	return websec.RequireUnsafeMethod(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		g, a, ok := p.admin(w, r)
		if !ok {
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, maxFormBytes)
		if err := r.ParseForm(); err != nil {
			p.show(w, r, g, a, http.StatusBadRequest, tabDraw, "ouvrir", keyInvalid, "")
			return
		}
		if r.PostForm.Get("confirm") != "1" {
			p.show(w, r, g, a, http.StatusUnprocessableEntity, tabDraw, "ouvrir", keyConfirm, "")
			return
		}
		if _, err := p.o.Reveal.OpenNow(r.Context(), a, g.ID, true); err != nil {
			key, st := errorKey(err)
			p.show(w, r, g, a, st, tabDraw, "ouvrir", key, "")
			return
		}
		p.record(r.Context(), a, g.ID, ActionOpen)
		http.Redirect(w, r, "/"+g.Slug+"/admin/tirage?ok=ouvert", http.StatusSeeOther)
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

// Routes liste les routes à brancher par l'orchestrateur ; elles attendent les
// middlewares SecurityHeaders, CSRF et Sessions.
func (p *Pages) Routes() []Route {
	return []Route{
		{"GET " + RouteDraw, p.DrawPage(), false},
		{"POST " + RouteLaunch, p.Launch(), true},
		{"POST " + RouteDate, p.SetDate(), true},
		{"POST " + RouteOpen, p.OpenNow(), true},
	}
}

// Register branche les routes sur mux.
func (p *Pages) Register(mux *http.ServeMux) {
	for _, r := range p.Routes() {
		mux.Handle(r.Pattern, r.Handler)
	}
}
