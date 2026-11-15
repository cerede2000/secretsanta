// Package adjustpages assemble la page « Ajuster le tirage » (F10c.2,
// screen-admin-ajuster) : retrait ou ajout d'une personne, aperçu sans aucun
// nom, confirmation, résultat, et proposition de tirage complet quand aucune
// réparation n'existe.
//
// Elle s'appuie sur le service d'ajustement de F4c (paquet adjust). Aucune
// paire n'est lue ni affichée : l'aperçu et le résultat ne portent que des
// comptes ; les noms affichés sont ceux de la liste des participants, jamais
// ceux d'une paire.
package adjustpages

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"html/template"
	"net/http"
	"strconv"

	"secretsanta/internal/adjust"
	"secretsanta/internal/drawengine"
	"secretsanta/internal/groups"
	"secretsanta/internal/i18n/negotiate"
	i18nweb "secretsanta/internal/i18n/web"
	"secretsanta/internal/participants"
	"secretsanta/internal/shared/adminnav"
	"secretsanta/internal/shared/errs"
	"secretsanta/internal/websec"
	"secretsanta/web"
)

// Groups est la part du service des groupes utilisée ici.
type Groups interface {
	BySlug(ctx context.Context, slug string) (*groups.Group, error)
}

// Adjuster est la part du service d'ajustement (F4c) utilisée ici.
type Adjuster interface {
	Preview(ctx context.Context, a participants.Actor, groupID int64, ch adjust.Change) (*adjust.Preview, error)
	Apply(ctx context.Context, a participants.Actor, groupID int64, ch adjust.Change, c adjust.Confirmation) (*adjust.Result, error)
}

// People est la part du service des participants utilisée ici.
type People interface {
	List(ctx context.Context, a participants.Actor, groupID int64) ([]participants.Participant, error)
}

// Membership dit qui est dans le tirage actuel, par identifiants seulement.
type Membership interface {
	InDraw(ctx context.Context, groupID int64) (map[int64]bool, error)
}

// SQLMembership lit l'appartenance au tirage actuel : les identifiants des
// personnes qui reçoivent un cadeau, sans jamais lire qui offre.
type SQLMembership struct{ DB *sql.DB }

// InDraw renvoie l'ensemble des personnes du tirage actuel du groupe.
func (m SQLMembership) InDraw(ctx context.Context, groupID int64) (map[int64]bool, error) {
	rows, err := m.DB.QueryContext(ctx, `SELECT DISTINCT p.receiver_id FROM draw_pairs p
		JOIN draws d ON d.id = p.draw_id WHERE d.group_id = ? AND d.status = 'actuel'`, groupID)
	if err != nil {
		return nil, errs.Internal(err)
	}
	defer rows.Close()
	out := map[int64]bool{}
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, errs.Internal(err)
		}
		out[id] = true
	}
	if err := rows.Err(); err != nil {
		return nil, errs.Internal(err)
	}
	return out, nil
}

// Auditor consigne une action sensible (journal de F10.1).
type Auditor interface {
	Record(ctx context.Context, a participants.Actor, groupID int64, action string, participantID int64) error
}

// Options rassemble les dépendances.
type Options struct {
	Groups     Groups
	Adjust     Adjuster
	People     People
	Membership Membership
	Translator i18nweb.Translator
	// Audit est facultatif.
	Audit Auditor
	// OnDrawChanged est appelé après un ajustement appliqué ; facultatif.
	OnDrawChanged func(ctx context.Context, groupID int64) error
	// NavFor donne les entrées de navigation d'administration ; facultatif.
	NavFor func(r *http.Request, active string) []adminnav.Item
}

// Pages construit les gestionnaires.
type Pages struct {
	o   Options
	tpl *template.Template
}

// Routes.
const (
	RouteAdjust = "/{slug}/admin/ajuster"

	maxFormBytes = 4 << 10
	navKey       = "tirage"
	// ActionAdjust est l'action consignée dans le journal (sans nom).
	ActionAdjust = "tirage_ajuste"

	modeRemove = "retirer"
	modeAdd    = "ajouter"
)

// Clés de texte (module admin.adjust).
const (
	keyTitle     = "admin.adjust.title"
	keyForbidden = "admin.adjust.error.forbidden"
	keyInternal  = "admin.adjust.error.internal"
	keyConflict  = "admin.adjust.error.conflict"
	keyChanged   = "admin.adjust.error.changed"
	keyInvalid   = "admin.adjust.error.invalid"
	keyConfirm   = "admin.adjust.error.confirm"
	keyNotFound  = "admin.adjust.error.not_found"
)

// NewPages analyse les gabarits.
func NewPages(o Options) (*Pages, error) {
	if o.Groups == nil || o.Adjust == nil || o.People == nil || o.Membership == nil || o.Translator == nil {
		return nil, errors.New("adjustpages: dépendance manquante")
	}
	funcs := template.FuncMap{}
	for k, v := range websec.TemplateFuncsPlaceholder() {
		funcs[k] = v
	}
	for k, v := range i18nweb.FuncsPlaceholder() {
		funcs[k] = v
	}
	t, err := template.New("").Funcs(funcs).ParseFS(web.Templates,
		"templates/layout/*.html", "templates/i18n/*.html", "templates/admin/adjust/*.html")
	if err != nil {
		return nil, fmt.Errorf("adjustpages: gabarit : %w", err)
	}
	return &Pages{o: o, tpl: t}, nil
}

type option struct {
	ID       int64
	Label    string
	Selected bool
}

type pageData struct {
	Lang     string
	TitleKey string
	Nav      []adminnav.Item
	Slug     string
	Mode     string

	Done    bool
	Options []option
	Chosen  int64

	Preview  *adjust.Preview
	Result   *adjust.Result
	Capacity bool
	CapCause string
	Missing  int
	// NoFix est vrai quand aucune réparation n'existe.
	NoFix bool
	// NoOne est vrai quand aucune personne n'est proposée.
	NoOne bool

	DrawHref string
	BackHref string
	ErrorKey string
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

func errorKey(err error) (string, int) {
	switch errs.KindOf(err) {
	case errs.KindForbidden, errs.KindUnauthorized:
		return keyForbidden, http.StatusForbidden
	case errs.KindConflict:
		return keyConflict, http.StatusConflict
	case errs.KindValidation:
		return keyInvalid, http.StatusUnprocessableEntity
	case errs.KindNotFound:
		return keyNotFound, http.StatusNotFound
	}
	return keyInternal, http.StatusInternalServerError
}

func modeOf(s string) string {
	if s == modeAdd {
		return modeAdd
	}
	return modeRemove
}

func kindOf(mode string) adjust.Kind {
	if mode == modeAdd {
		return adjust.KindAdd
	}
	return adjust.KindRemove
}

var causeSuffix = map[drawengine.Cause]string{
	drawengine.CauseTooManyOneGift: "too_many_one_gift",
	drawengine.CauseCapTooLow:      "cap_too_low",
	drawengine.CauseTooFewGivers:   "too_few_givers",
}

// base prépare les données communes : la liste des personnes proposées.
func (p *Pages) base(ctx context.Context, g *groups.Group, a participants.Actor, mode string, chosen int64) (*pageData, error) {
	d := &pageData{Slug: g.Slug, Mode: mode, Chosen: chosen,
		DrawHref: "/" + g.Slug + "/admin/tirage", BackHref: "/" + g.Slug + "/admin/participants"}
	in, err := p.o.Membership.InDraw(ctx, g.ID)
	if err != nil {
		return nil, err
	}
	d.Done = len(in) > 0
	if !d.Done {
		return d, nil
	}
	list, err := p.o.People.List(ctx, a, g.ID)
	if err != nil {
		return nil, err
	}
	for _, pe := range list {
		if in[pe.ID] == (mode == modeRemove) {
			d.Options = append(d.Options, option{ID: pe.ID, Label: pe.FirstName + " " + pe.Name, Selected: pe.ID == chosen})
		}
	}
	d.NoOne = len(d.Options) == 0
	return d, nil
}

func (p *Pages) fail(w http.ResponseWriter, r *http.Request, g *groups.Group, a participants.Actor, mode string, status int, key string) {
	d, err := p.base(r.Context(), g, a, mode, 0)
	if err != nil {
		d = &pageData{Slug: g.Slug, Mode: mode, DrawHref: "/" + g.Slug + "/admin/tirage", BackHref: "/" + g.Slug + "/admin/participants"}
	}
	d.ErrorKey = key
	p.render(w, r, status, *d)
}

// explainRefusal reporte la cause d'un refus dans les données, sans texte du
// service (les textes viennent des catalogues).
func explainRefusal(d *pageData, rf *adjust.Refusal) {
	d.NoFix = true
	if rf == nil {
		return
	}
	d.Capacity, d.Missing = rf.Capacity, rf.Missing
	d.CapCause = "other"
	if s, ok := causeSuffix[rf.Cause]; ok {
		d.CapCause = s
	}
}

// Page rend screen-admin-ajuster (GET /{slug}/admin/ajuster). Elle ne modifie
// rien : « mode=retirer|ajouter » choisit l'onglet, « personne » demande
// l'aperçu.
func (p *Pages) Page() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		g, a, ok := p.admin(w, r)
		if !ok {
			return
		}
		mode := modeOf(r.URL.Query().Get("mode"))
		chosen, _ := strconv.ParseInt(r.URL.Query().Get("personne"), 10, 64)
		d, err := p.base(r.Context(), g, a, mode, chosen)
		if err != nil {
			key, st := errorKey(err)
			p.fail(w, r, g, a, mode, st, key)
			return
		}
		status := http.StatusOK
		if d.Done && chosen > 0 {
			pv, err := p.o.Adjust.Preview(r.Context(), a, g.ID, adjust.Change{Kind: kindOf(mode), ParticipantID: chosen})
			if err != nil {
				key, st := errorKey(err)
				d.ErrorKey, status = key, st
			} else {
				d.Preview = pv
				if !pv.Possible {
					explainRefusal(d, pv.Refusal)
				}
			}
		}
		p.render(w, r, status, *d)
	})
}

// Apply applique l'ajustement confirmé (POST /{slug}/admin/ajuster).
func (p *Pages) Apply() http.Handler {
	return websec.RequireUnsafeMethod(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		g, a, ok := p.admin(w, r)
		if !ok {
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, maxFormBytes)
		if err := r.ParseForm(); err != nil {
			p.fail(w, r, g, a, modeRemove, http.StatusBadRequest, keyInvalid)
			return
		}
		mode := modeOf(r.PostForm.Get("mode"))
		id, err1 := strconv.ParseInt(r.PostForm.Get("personne"), 10, 64)
		rev, err2 := strconv.Atoi(r.PostForm.Get("revision"))
		if err1 != nil || err2 != nil || id <= 0 {
			p.fail(w, r, g, a, mode, http.StatusUnprocessableEntity, keyInvalid)
			return
		}
		if r.PostForm.Get("confirm") != "1" {
			p.fail(w, r, g, a, mode, http.StatusUnprocessableEntity, keyConfirm)
			return
		}
		ch := adjust.Change{Kind: kindOf(mode), ParticipantID: id}
		res, err := p.o.Adjust.Apply(r.Context(), a, g.ID, ch, adjust.Confirmation{Confirmed: true, Revision: rev})
		if err != nil {
			var rf *adjust.Refusal
			if errors.As(err, &rf) {
				d, berr := p.base(r.Context(), g, a, mode, id)
				if berr != nil {
					d = &pageData{Slug: g.Slug, Mode: mode, DrawHref: "/" + g.Slug + "/admin/tirage", BackHref: "/" + g.Slug + "/admin/participants"}
				}
				explainRefusal(d, rf)
				p.render(w, r, http.StatusConflict, *d)
				return
			}
			key, st := errorKey(err)
			if st == http.StatusConflict {
				key = keyChanged
			}
			p.fail(w, r, g, a, mode, st, key)
			return
		}
		if p.o.Audit != nil {
			_ = p.o.Audit.Record(r.Context(), a, g.ID, ActionAdjust, 0)
		}
		if p.o.OnDrawChanged != nil {
			_ = p.o.OnDrawChanged(r.Context(), g.ID)
		}
		d, berr := p.base(r.Context(), g, a, mode, 0)
		if berr != nil {
			d = &pageData{Slug: g.Slug, Mode: mode, Done: true, DrawHref: "/" + g.Slug + "/admin/tirage", BackHref: "/" + g.Slug + "/admin/participants"}
		}
		d.Result = res
		p.render(w, r, http.StatusOK, *d)
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
		{"GET " + RouteAdjust, p.Page(), false},
		{"POST " + RouteAdjust, p.Apply(), true},
	}
}

// Register branche les routes sur mux.
func (p *Pages) Register(mux *http.ServeMux) {
	for _, r := range p.Routes() {
		mux.Handle(r.Pattern, r.Handler)
	}
}
