// Package suggestionpages porte la page du tireur (screen-suggestions, F8c.2) :
// la liste des idées de cadeau d'une cible avec image locale, prix et lien,
// l'actualisation, le filtre de prix libre et, pour un tireur à deux cibles, le
// sélecteur de cadeau.
//
// Règles : seule la personne connectée voit ses propres cibles, jamais avant
// la découverte de la cible ; une personne en réception seule n'a pas de page ;
// le filtre de prix est lu en GET (il ne modifie rien), l'actualisation est un
// POST enveloppé par websec.RequireUnsafeMethod. Le paquet ne connaît ni les
// routes ni l'analyse des gabarits : l'orchestrateur fournit un Renderer.
package suggestionpages

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"secretsanta/internal/access/limiter"
	"secretsanta/internal/draw"
	"secretsanta/internal/groups"
	"secretsanta/internal/i18n/negotiate"
	"secretsanta/internal/mail/templates"
	"secretsanta/internal/participants"
	"secretsanta/internal/shared/errs"
	"secretsanta/internal/suggestions"
	"secretsanta/internal/websec"
)

// Noms des gabarits (web/templates/suggestions/*.html) à analyser avec le layout.
const (
	TemplatePage  = "suggestions/page"
	TemplateError = "suggestions/error"
)

const maxFormBytes = 4 << 10

// GroupSource : *groups.Service.
type GroupSource interface {
	BySlug(ctx context.Context, slug string) (*groups.Group, error)
}

// ParticipantSource : *participants.Service.
type ParticipantSource interface {
	Get(ctx context.Context, a participants.Actor, groupID, id int64) (*participants.Participant, error)
}

// DrawSource : *draw.Service.
type DrawSource interface {
	MyTargets(ctx context.Context, a participants.Actor, groupID int64) (*draw.MyTargets, error)
}

// Suggester : *suggestions.Service.
type Suggester interface {
	Suggest(ctx context.Context, r suggestions.Request) (*suggestions.Result, error)
}

// Renderer écrit la page name avec le statut donné (fourni par l'orchestrateur).
type Renderer func(w http.ResponseWriter, r *http.Request, status int, name string, data any)

// Deps regroupe les dépendances des pages.
type Deps struct {
	Groups       GroupSource
	Participants ParticipantSource
	Draw         DrawSource
	Suggest      Suggester
	Render       Renderer
	// DefaultLang sert si la langue n'a pas été négociée (« fr » si vide).
	DefaultLang string
	// Chemins ; les valeurs par défaut sont indiquées.
	// IdeasPath : « /<slug>/suggestions/<paire> ».
	IdeasPath func(slug string, pairID int64) string
	// LetterPath : « /<slug>/lettre-recue/<paire> ».
	LetterPath func(slug string, pairID int64) string
	// DiscoveryPath : « /<slug>/decouverte ».
	DiscoveryPath func(slug string) string
	// HomePath : « /<slug>/participant ».
	HomePath func(slug string) string
}

// Gift est un onglet du sélecteur de cadeau.
type Gift struct {
	N       int
	Name    string
	URL     string
	Current bool
}

// Chip est une pastille de filtre par budget.
type Chip struct {
	// Kind : all, up_to ou between.
	Kind     string
	Min, Max string
	URL      string
	On       bool
}

// ItemView est une proposition prête à afficher. Image est un chemin local.
type ItemView struct {
	Title, URL, Image, Price, Source string
}

// Page est la donnée des gabarits du paquet.
type Page struct {
	Lang     string
	TitleKey string
	ErrorKey string
	BackURL  string

	LetterURL string
	Name      string
	Gifts     []Gift
	HasBudget bool
	Budget    string
	Chips     []Chip

	Items       []ItemView
	FromLetter  bool
	Throttled   bool
	Unavailable bool
	Empty       bool

	RefreshAction string
	FilterAction  string
	Min, Max      string
	MinErrKey     string
	MaxErrKey     string
}

// Pages construit les gestionnaires.
type Pages struct{ d Deps }

// New valide les dépendances et pose les valeurs par défaut.
func New(d Deps) (*Pages, error) {
	if d.Groups == nil || d.Participants == nil || d.Draw == nil || d.Suggest == nil || d.Render == nil {
		return nil, errors.New("suggestionpages : Groups, Participants, Draw, Suggest et Render sont obligatoires")
	}
	if d.DefaultLang == "" {
		d.DefaultLang = "fr"
	}
	pair := func(prefix string) func(string, int64) string {
		return func(slug string, id int64) string { return "/" + slug + prefix + strconv.FormatInt(id, 10) }
	}
	if d.IdeasPath == nil {
		d.IdeasPath = pair("/suggestions/")
	}
	if d.LetterPath == nil {
		d.LetterPath = pair("/lettre-recue/")
	}
	if d.DiscoveryPath == nil {
		d.DiscoveryPath = func(slug string) string { return "/" + slug + "/decouverte" }
	}
	if d.HomePath == nil {
		d.HomePath = func(slug string) string { return "/" + slug + "/participant" }
	}
	return &Pages{d: d}, nil
}

func (p *Pages) lang(r *http.Request) string {
	return negotiate.Lang(r.Context(), p.d.DefaultLang)
}

func noStore(w http.ResponseWriter) { w.Header().Set("Cache-Control", "no-store") }

// Show sert la page (GET, ne modifie rien), à monter sur
// « GET /{slug}/suggestions/{pair} ».
func (p *Pages) Show() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		noStore(w)
		c, ok := p.resolve(w, r)
		if !ok {
			return
		}
		p.render(w, r, c, false, http.StatusOK, "")
	})
}

// Refresh actualise les idées (POST seulement : un GET renvoie 405), à monter
// sur « /{slug}/suggestions/{pair}/actualiser ». Elle ignore le cache puis
// renvoie vers la page.
func (p *Pages) Refresh() http.Handler {
	return websec.RequireUnsafeMethod(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		noStore(w)
		c, ok := p.resolve(w, r)
		if !ok {
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, maxFormBytes)
		if err := r.ParseForm(); err != nil {
			p.render(w, r, c, false, http.StatusBadRequest, "suggestions.error.not_found")
			return
		}
		c.rawMin, c.rawMax = strings.TrimSpace(r.PostFormValue("min")), strings.TrimSpace(r.PostFormValue("max"))
		p.render(w, r, c, true, http.StatusOK, "")
	}))
}

// target est la cible résolue de la requête.
type target struct {
	g       *groups.Group
	me      *participants.Participant
	index   int // 0 ou 1
	pairID  int64
	targets []draw.Target
	name    string
	rawMin  string
	rawMax  string
}

// resolve vérifie la session, le rôle, la révélation et l'appartenance de la
// paire au tireur. Quand ok est faux, la réponse est écrite.
func (p *Pages) resolve(w http.ResponseWriter, r *http.Request) (*target, bool) {
	rctx := r.Context()
	g, err := p.d.Groups.BySlug(rctx, r.PathValue("slug"))
	if err != nil {
		status, key := http.StatusNotFound, "suggestions.error.not_found"
		if !errs.Is(err, errs.KindNotFound) {
			status, key = http.StatusInternalServerError, "core.errors.internal"
		}
		p.fail(w, r, status, key)
		return nil, false
	}
	login := func() { http.Redirect(w, r, "/"+g.Slug+"/connexion", http.StatusSeeOther) }
	sess, found := websec.ParticipantSession(rctx, strconv.FormatInt(g.ID, 10))
	if !found {
		login()
		return nil, false
	}
	pid, err := strconv.ParseInt(sess.Subject, 10, 64)
	if err != nil || pid <= 0 {
		login()
		return nil, false
	}
	actor := participants.ParticipantActor(g.ID, pid)
	me, err := p.d.Participants.Get(rctx, actor, g.ID, pid)
	if err != nil {
		if errs.Is(err, errs.KindNotFound) || errs.Is(err, errs.KindUnauthorized) || errs.Is(err, errs.KindForbidden) {
			login()
			return nil, false
		}
		p.fail(w, r, http.StatusInternalServerError, "core.errors.internal")
		return nil, false
	}
	if me.Role == participants.RoleReceptionSeule {
		p.fail(w, r, http.StatusForbidden, "suggestions.error.no_page")
		return nil, false
	}
	pairID, err := strconv.ParseInt(r.PathValue("pair"), 10, 64)
	if err != nil || pairID <= 0 {
		p.fail(w, r, http.StatusNotFound, "suggestions.error.not_found")
		return nil, false
	}
	mine, err := p.d.Draw.MyTargets(rctx, actor, g.ID)
	if err != nil {
		p.fail(w, r, http.StatusInternalServerError, "core.errors.internal")
		return nil, false
	}
	if mine == nil || !mine.Ready {
		http.Redirect(w, r, p.d.HomePath(g.Slug), http.StatusSeeOther)
		return nil, false
	}
	if len(mine.Targets) == 0 {
		p.fail(w, r, http.StatusForbidden, "suggestions.error.no_page")
		return nil, false
	}
	idx := -1
	for i, t := range mine.Targets {
		if t.PairID == pairID {
			idx = i
		}
	}
	if idx < 0 {
		// Même réponse qu'une paire inexistante : on ne révèle rien d'autrui.
		p.fail(w, r, http.StatusNotFound, "suggestions.error.not_found")
		return nil, false
	}
	if !mine.Targets[idx].Discovered {
		http.Redirect(w, r, p.d.DiscoveryPath(g.Slug), http.StatusSeeOther)
		return nil, false
	}
	rec, err := p.d.Participants.Get(rctx, participants.AdminActor(g.ID), g.ID, mine.Targets[idx].ReceiverID)
	if err != nil {
		p.fail(w, r, http.StatusInternalServerError, "core.errors.internal")
		return nil, false
	}
	name := strings.TrimSpace(rec.FirstName)
	if name == "" {
		name = strings.TrimSpace(rec.Name)
	}
	c := &target{g: g, me: me, index: idx, pairID: pairID, targets: mine.Targets, name: name,
		rawMin: strings.TrimSpace(r.URL.Query().Get("min")), rawMax: strings.TrimSpace(r.URL.Query().Get("max"))}
	return c, true
}

func (p *Pages) fail(w http.ResponseWriter, r *http.Request, status int, key string) {
	p.d.Render(w, r, status, TemplateError, Page{Lang: p.lang(r), TitleKey: "suggestions.error.title", ErrorKey: key})
}

var euroRe = regexp.MustCompile(`^\d{1,6}(?:[.,]\d{1,2})?$`)

// parseEuros lit « 12 », « 12,5 » ou « 12.50 » ; vide : pas de borne.
func parseEuros(s string) (*int64, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil, true
	}
	if !euroRe.MatchString(s) {
		return nil, false
	}
	whole, frac, _ := strings.Cut(strings.ReplaceAll(s, ",", "."), ".")
	w, err := strconv.ParseInt(whole, 10, 64)
	if err != nil {
		return nil, false
	}
	cents := w * 100
	if frac != "" {
		f, err := strconv.ParseInt((frac + "0")[:2], 10, 64)
		if err != nil {
			return nil, false
		}
		cents += f
	}
	return &cents, true
}

// render calcule les idées (ou actualise) puis écrit la page. Un filtre de prix
// invalide ne lance aucune recherche : le message s'affiche sous le champ.
func (p *Pages) render(w http.ResponseWriter, r *http.Request, c *target, refresh bool, status int, errKey string) {
	lang := p.lang(r)
	g := c.g
	pg := Page{Lang: lang, TitleKey: "suggestions.title", ErrorKey: errKey,
		LetterURL: p.d.LetterPath(g.Slug, c.pairID), Name: c.name,
		Min: c.rawMin, Max: c.rawMax,
		FilterAction: p.d.IdeasPath(g.Slug, c.pairID), RefreshAction: p.d.IdeasPath(g.Slug, c.pairID) + "/actualiser",
		HasBudget: g.BudgetCents != nil}
	if g.BudgetCents != nil {
		pg.Budget = templates.FormatBudget(lang, *g.BudgetCents)
	}
	pg.Gifts = p.gifts(r, c)

	minC, okMin := parseEuros(c.rawMin)
	maxC, okMax := parseEuros(c.rawMax)
	switch {
	case !okMin:
		pg.MinErrKey = "suggestions.free.error.min"
	case !okMax:
		pg.MaxErrKey = "suggestions.free.error.max"
	case minC != nil && maxC != nil && *minC > *maxC:
		pg.MaxErrKey = "suggestions.free.error.order"
	}
	if pg.MinErrKey != "" || pg.MaxErrKey != "" {
		pg.Chips = p.chips(lang, g, c, "", "")
		p.d.Render(w, r, http.StatusBadRequest, TemplatePage, pg)
		return
	}
	pg.Chips = p.chips(lang, g, c, c.rawMin, c.rawMax)

	res, err := p.d.Suggest.Suggest(r.Context(), suggestions.Request{
		Actor: participants.ParticipantActor(g.ID, c.me.ID), GroupID: g.ID, Gift: c.index + 1,
		MinCents: minC, MaxCents: maxC, Refresh: refresh, Lang: lang})
	if err != nil {
		switch {
		case errors.Is(err, limiter.ErrLimited):
			pg.ErrorKey, status = "suggestions.error.limited", http.StatusTooManyRequests
		case errs.Is(err, errs.KindForbidden):
			p.fail(w, r, http.StatusForbidden, "suggestions.error.no_page")
			return
		case errs.Is(err, errs.KindConflict):
			http.Redirect(w, r, p.d.HomePath(g.Slug), http.StatusSeeOther)
			return
		case errs.Is(err, errs.KindNotFound):
			p.fail(w, r, http.StatusNotFound, "suggestions.error.not_found")
			return
		case errs.Is(err, errs.KindValidation):
			pg.MaxErrKey, status = "suggestions.free.error.order", http.StatusBadRequest
		default:
			p.fail(w, r, http.StatusInternalServerError, "core.errors.internal")
			return
		}
		p.d.Render(w, r, status, TemplatePage, pg)
		return
	}
	if refresh && !res.Throttled {
		// Les idées sont en cache : on revient sur la page (jamais de POST rejoué).
		http.Redirect(w, r, p.pageURL(g, c, c.rawMin, c.rawMax), http.StatusSeeOther)
		return
	}
	pg.FromLetter, pg.Throttled, pg.Unavailable = res.FromLetter, res.Throttled, res.Unavailable
	for _, it := range res.Items {
		v := ItemView{Title: it.Title, URL: it.URL, Image: it.ImageSrc, Source: it.Source}
		if it.PriceCents > 0 {
			v.Price = templates.FormatBudget(lang, it.PriceCents)
		}
		pg.Items = append(pg.Items, v)
	}
	pg.Empty = !res.Unavailable && len(res.Items) == 0
	p.d.Render(w, r, status, TemplatePage, pg)
}

func (p *Pages) gifts(r *http.Request, c *target) []Gift {
	if len(c.targets) < 2 {
		return nil
	}
	var out []Gift
	for i, t := range c.targets {
		g := Gift{N: i + 1, URL: p.d.IdeasPath(c.g.Slug, t.PairID), Current: i == c.index}
		if t.Discovered {
			if rec, err := p.d.Participants.Get(r.Context(), participants.AdminActor(c.g.ID), c.g.ID, t.ReceiverID); err == nil {
				g.Name = strings.TrimSpace(rec.FirstName)
				if g.Name == "" {
					g.Name = strings.TrimSpace(rec.Name)
				}
			}
		} else {
			// Une cible à découvrir n'a ni nom ni page : le lien mène à la découverte.
			g.URL = p.d.DiscoveryPath(c.g.Slug)
		}
		out = append(out, g)
	}
	return out
}

func (p *Pages) pageURL(g *groups.Group, c *target, min, max string) string {
	u := p.d.IdeasPath(g.Slug, c.pairID)
	q := url.Values{}
	if min != "" {
		q.Set("min", min)
	}
	if max != "" {
		q.Set("max", max)
	}
	if len(q) > 0 {
		u += "?" + q.Encode()
	}
	return u
}

// chips propose, quand le groupe a un budget, trois pastilles : tout, jusqu'à
// 60 % du budget, de 60 % au budget. Elles s'appuient sur le même filtre libre.
func (p *Pages) chips(lang string, g *groups.Group, c *target, curMin, curMax string) []Chip {
	if g.BudgetCents == nil {
		return nil
	}
	budget := *g.BudgetCents
	split := (budget * 6 / 10) / 100 * 100 // euros entiers
	chips := []Chip{{Kind: "all", URL: p.pageURL(g, c, "", ""), On: curMin == "" && curMax == ""}}
	if split <= 0 || split >= budget {
		return chips
	}
	sv, bv := centsParam(split), centsParam(budget)
	return append(chips,
		Chip{Kind: "up_to", Max: templates.FormatBudget(lang, split), URL: p.pageURL(g, c, "", sv),
			On: curMin == "" && curMax == sv},
		Chip{Kind: "between", Min: templates.FormatBudget(lang, split), Max: templates.FormatBudget(lang, budget),
			URL: p.pageURL(g, c, sv, bv), On: curMin == sv && curMax == bv})
}

// centsParam écrit des centimes comme paramètre d'adresse : « 25 » ou « 24.99 ».
func centsParam(cents int64) string {
	if cents%100 == 0 {
		return strconv.FormatInt(cents/100, 10)
	}
	return strconv.FormatInt(cents/100, 10) + "." + (strconv.FormatInt(100+cents%100, 10))[1:]
}
