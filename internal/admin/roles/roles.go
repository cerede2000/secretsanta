// Package roles porte le réglage du rôle dans le tirage côté administrateur
// (F10.6) : le sélecteur à quatre valeurs, le filtre et le badge réutilisés
// par la fiche et la liste des participants, et le panneau « Rôles dans le
// tirage » (modification en lot et récapitulatif en comptes).
//
// Aucune page n'affiche une paire ni une cible : le récapitulatif ne contient
// que des comptes. Après le tirage, un changement de rôle passe par le service
// rulechange (F3d) : aperçu en comptes, réparation minimale, jamais de nouveau
// tirage silencieux. Les gestionnaires sont construits ici et branchés par
// l'orchestrateur (voir Routes).
package roles

import (
	"context"
	"errors"
	"fmt"
	"html/template"
	"log/slog"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"

	"secretsanta/internal/draw"
	"secretsanta/internal/drawengine"
	"secretsanta/internal/groups"
	"secretsanta/internal/i18n/negotiate"
	i18nweb "secretsanta/internal/i18n/web"
	"secretsanta/internal/participants"
	"secretsanta/internal/rulechange"
	"secretsanta/internal/shared/adminnav"
	"secretsanta/internal/shared/errs"
	"secretsanta/internal/websec"
	"secretsanta/web"
)

// Groups est la part du service des groupes utilisée ici.
type Groups interface {
	BySlug(ctx context.Context, slug string) (*groups.Group, error)
}

// People est la part du service des participants utilisée ici.
type People interface {
	List(ctx context.Context, a participants.Actor, groupID int64) ([]participants.Participant, error)
}

// RoleSetter est la part du service rulechange (F3d) utilisée ici.
type RoleSetter interface {
	SetRole(ctx context.Context, groupID, participantID int64, role participants.DrawRole, choice rulechange.Choice) (rulechange.Outcome, error)
}

// Checker est la part du service du tirage (F4) utilisée ici : contrôle de
// capacité sans tirer, comptes seulement.
type Checker interface {
	Check(ctx context.Context, a participants.Actor, groupID int64, capConfirmed bool) (*draw.CheckReport, error)
}

// DrawState est le port d'état du tirage.
type DrawState interface {
	IsDrawDone(ctx context.Context, groupID int64) (bool, error)
}

// Options rassemble les dépendances.
type Options struct {
	Groups     Groups
	People     People
	Roles      RoleSetter
	Check      Checker
	State      DrawState
	Translator i18nweb.Translator

	// Nav donne les entrées de la navigation d'administration (registre de
	// F1b) ; facultatif (barre vide sinon).
	Nav func(r *http.Request, activeKey string) []adminnav.Item
	// Journal consigne une action sensible (journal de F10.1) ; facultatif.
	// Les détails ne contiennent que des comptes.
	Journal func(ctx context.Context, groupID int64, action string, counts map[string]int)
}

// Pages construit les gestionnaires.
type Pages struct {
	o   Options
	tpl *template.Template
}

// Clés et noms.
const (
	titleKey       = "admin.roles.title"
	navKey         = "tirage"
	formIDs        = "ids"
	formRole       = "role"
	formChoice     = "choice"
	formNext       = "next"
	choiceRepair   = "repair"
	choiceFuture   = "future"
	maxFormBytes   = 64 << 10
	maxSelection   = 1000
	ActionRoleSet  = "roles.changement"
	errInvalidRole = "admin.roles.error.invalid_role"
	errNoSelection = "admin.roles.error.no_selection"
	errUnknown     = "admin.roles.error.unknown_person"
	errInternal    = "admin.roles.error.internal"
	errForm        = "admin.roles.error.form"
)

// Statuts de capacité du récapitulatif.
const (
	StatusOK    = "ok"
	StatusShort = "short"
	StatusCheck = "check"
)

// RoleOption est une valeur du sélecteur.
type RoleOption struct {
	Value    string
	LabelKey string
	HelpKey  string
	Selected bool
}

var allRoles = []participants.DrawRole{
	participants.RoleNormal, participants.RoleReceptionSeule,
	participants.RoleOffrePlus, participants.RoleUnSeul,
}

// Options renvoie les quatre valeurs exclusives dans l'ordre d'affichage,
// celle de current étant sélectionnée.
func RoleOptions(current string) []RoleOption {
	out := make([]RoleOption, 0, len(allRoles))
	for _, r := range allRoles {
		out = append(out, RoleOption{
			Value: string(r), LabelKey: "admin.roles.role." + string(r), HelpKey: "admin.roles.help." + string(r),
			Selected: string(r) == current,
		})
	}
	return out
}

// Selector est la donnée du fragment « admin/roles/selector » (fiche ou
// liste) : un formulaire qui envoie le rôle choisi à la page des rôles.
type Selector struct {
	Slug    string
	ID      int64
	Name    string
	Role    string
	Options []RoleOption
	// Next est la page où revenir après le changement (chemin local de
	// l'administration du même groupe) ; vide = le panneau des rôles.
	Next string
	// Suffix rend uniques les identifiants des éléments (plusieurs
	// sélecteurs sur une page).
	Suffix string
}

// NewSelector prépare le fragment pour une personne.
func NewSelector(slug string, id int64, name string, role participants.DrawRole, next string) Selector {
	return Selector{Slug: slug, ID: id, Name: name, Role: string(role), Options: RoleOptions(string(role)),
		Next: next, Suffix: strconv.FormatInt(id, 10)}
}

// Badge est la donnée du fragment « admin/roles/badge ».
type Badge struct {
	Role     string
	LabelKey string
}

// NewBadge prépare le badge d'un rôle.
func NewBadge(role participants.DrawRole) Badge {
	return Badge{Role: string(role), LabelKey: "admin.roles.role." + string(role)}
}

// Filter est la donnée du fragment « admin/roles/filter ».
type Filter struct {
	Action  string
	Current string
	Options []RoleOption
}

// NewFilter prépare le filtre par rôle d'une liste (formulaire GET, champ
// « role » ; valeur vide = tous les rôles).
func NewFilter(action string, current string) Filter {
	return Filter{Action: action, Current: current, Options: RoleOptions(current)}
}

// ParseRole lit un rôle de formulaire ; ok est faux hors des quatre valeurs.
func ParseRole(s string) (participants.DrawRole, bool) {
	r := participants.DrawRole(s)
	return r, r.Valid()
}

// FilterByRole garde les personnes du rôle donné ; un rôle vide ou inconnu
// garde tout le monde.
func FilterByRole(list []participants.Participant, role string) []participants.Participant {
	r, ok := ParseRole(role)
	if !ok {
		return list
	}
	out := make([]participants.Participant, 0, len(list))
	for _, p := range list {
		if p.Role == r {
			out = append(out, p)
		}
	}
	return out
}

// New analyse les gabarits.
func New(o Options) (*Pages, error) {
	if o.Groups == nil || o.People == nil || o.Roles == nil || o.Check == nil || o.State == nil || o.Translator == nil {
		return nil, errors.New("roles: dépendance manquante")
	}
	funcs := template.FuncMap{}
	for k, v := range websec.TemplateFuncsPlaceholder() {
		funcs[k] = v
	}
	for k, v := range i18nweb.FuncsPlaceholder() {
		funcs[k] = v
	}
	t, err := template.New("").Funcs(funcs).ParseFS(web.Templates,
		"templates/layout/*.html", "templates/i18n/*.html", "templates/admin/roles/*.html")
	if err != nil {
		return nil, fmt.Errorf("roles: gabarits : %w", err)
	}
	return &Pages{o: o, tpl: t}, nil
}

// row est une personne du panneau.
type row struct {
	Selector Selector
	Badge    Badge
	Name     string
	ID       int64
}

// summary est le récapitulatif en comptes (aucun nom).
type summary struct {
	Status      string
	Extra       int
	OffrePlus   int
	Normal      int
	UnSeul      int
	Reception   int
	Capacity    int
	Missing     int
	CauseKey    string
	Beneficiary int
}

type confirm struct {
	IDs        []int64
	Role       string
	RoleKey    string
	Changes    int
	Affected   int
	Discovered int
}

type view struct {
	Lang, TitleKey string
	Nav            []adminnav.Item
	Slug           string
	GroupName      string
	Rows           []row
	Sum            summary
	Options        []RoleOption
	DrawDone       bool
	Confirm        *confirm
	ErrorKey       string
	NoticeApplied  int
	NoticeFuture   int
	NoticeRepaired int
	BackURL        string
	SettingsURL    string
	Action         string
}

func (p *Pages) admin(w http.ResponseWriter, r *http.Request) (*groups.Group, bool) {
	g, err := p.o.Groups.BySlug(r.Context(), r.PathValue("slug"))
	if err != nil || g == nil {
		http.Error(w, "404 page not found", http.StatusNotFound)
		return nil, false
	}
	if _, ok := websec.AdminSession(r.Context(), strconv.FormatInt(g.ID, 10)); !ok {
		http.Redirect(w, r, "/"+g.Slug+"/admin/connexion", http.StatusSeeOther)
		return nil, false
	}
	return g, true
}

func displayName(p participants.Participant) string {
	return strings.TrimSpace(p.FirstName + " " + p.Name)
}

func (p *Pages) build(ctx context.Context, r *http.Request, g *groups.Group) (*view, []participants.Participant, error) {
	a := participants.AdminActor(g.ID)
	list, err := p.o.People.List(ctx, a, g.ID)
	if err != nil {
		return nil, nil, err
	}
	sort.SliceStable(list, func(i, j int) bool {
		return strings.ToLower(displayName(list[i])) < strings.ToLower(displayName(list[j]))
	})
	done, err := p.o.State.IsDrawDone(ctx, g.ID)
	if err != nil {
		return nil, nil, err
	}
	v := &view{
		TitleKey: titleKey, Slug: g.Slug, GroupName: g.Name, DrawDone: done,
		Options: RoleOptions(""), Action: "/" + g.Slug + "/admin/roles",
		BackURL: "/" + g.Slug + "/admin/tirage", SettingsURL: "/" + g.Slug + "/admin/reglages",
	}
	if p.o.Nav != nil {
		v.Nav = p.o.Nav(r, navKey)
	}
	for _, pt := range list {
		n := displayName(pt)
		v.Rows = append(v.Rows, row{ID: pt.ID, Name: n, Badge: NewBadge(pt.Role),
			Selector: NewSelector(g.Slug, pt.ID, n, pt.Role, "")})
	}
	v.Sum = p.summarize(ctx, g, list)
	return v, list, nil
}

// summarize compte les rôles et juge la capacité. Le contrôle du moteur (F4)
// fait foi ; ses explications sont reprises par cause et par comptes, jamais
// par leur texte.
func (p *Pages) summarize(ctx context.Context, g *groups.Group, list []participants.Participant) summary {
	var s summary
	for _, pt := range list {
		switch pt.Role {
		case participants.RoleReceptionSeule:
			s.Reception++
		case participants.RoleOffrePlus:
			s.OffrePlus++
		case participants.RoleUnSeul:
			s.UnSeul++
		default:
			s.Normal++
		}
	}
	s.Beneficiary = len(list)
	capN := g.MaxGiftsPerGiver
	if capN <= 0 {
		capN = drawengine.DefaultCap
	}
	s.Extra = s.Reception
	s.Capacity = (s.OffrePlus + s.Normal) * max(capN-1, 0)
	s.Status = StatusCheck
	rep, err := p.o.Check.Check(ctx, participants.AdminActor(g.ID), g.ID, true)
	if rep != nil {
		s.Extra = rep.Capacity.ExtraGiftsNeeded
		s.Capacity = rep.Capacity.TotalCapacity
	}
	if err == nil && rep != nil {
		s.Status = StatusOK
		return s
	}
	code, params := errs.CodeOf(err)
	if code != draw.CodeInfeasible {
		return s
	}
	cause, _ := params["cause"].(string)
	switch drawengine.Cause(cause) {
	case drawengine.CauseTooManyOneGift, drawengine.CauseCapTooLow:
		s.Status = StatusShort
		s.CauseKey = "admin.roles.short." + cause
		s.Missing, _ = params["missing"].(int)
	}
	return s
}

func (p *Pages) render(w http.ResponseWriter, r *http.Request, status int, v *view) {
	clone, err := p.tpl.Clone()
	if err != nil {
		http.Error(w, "Internal error", http.StatusInternalServerError)
		return
	}
	clone.Funcs(websec.TemplateFuncs(r)).Funcs(i18nweb.Funcs(p.o.Translator, r))
	v.Lang = negotiate.Lang(r.Context(), p.o.Translator.DefaultLang())
	var buf strings.Builder
	if err := clone.ExecuteTemplate(&buf, "layout/admin", v); err != nil {
		slog.Error("roles : rendu", "err", err)
		http.Error(w, "Internal error", http.StatusInternalServerError)
		return
	}
	h := w.Header()
	h.Set("Content-Type", "text/html; charset=utf-8")
	h.Set("Cache-Control", "no-store")
	h.Set("Referrer-Policy", "no-referrer")
	w.WriteHeader(status)
	_, _ = w.Write([]byte(buf.String()))
}

func (p *Pages) fail(w http.ResponseWriter, r *http.Request, g *groups.Group, status int, key string) {
	v, _, err := p.build(r.Context(), r, g)
	if err != nil {
		slog.Error("roles : lecture", "err", err)
		http.Error(w, "Internal error", http.StatusInternalServerError)
		return
	}
	v.ErrorKey = key
	p.render(w, r, status, v)
}

// Panel rend le panneau « Rôles dans le tirage » (GET /{slug}/admin/roles).
func (p *Pages) Panel() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		g, ok := p.admin(w, r)
		if !ok {
			return
		}
		v, _, err := p.build(r.Context(), r, g)
		if err != nil {
			slog.Error("roles : lecture", "err", err)
			http.Error(w, "Internal error", http.StatusInternalServerError)
			return
		}
		q := r.URL.Query()
		v.NoticeApplied = smallInt(q.Get("applied"))
		v.NoticeFuture = smallInt(q.Get("future"))
		v.NoticeRepaired = smallInt(q.Get("repaired"))
		p.render(w, r, http.StatusOK, v)
	})
}

func smallInt(s string) int {
	n, err := strconv.Atoi(s)
	if err != nil || n < 0 || n > maxSelection {
		return 0
	}
	return n
}

// safeNext n'accepte qu'un chemin d'administration du même groupe.
func safeNext(slug, next string) string {
	prefix := "/" + slug + "/admin/"
	if !strings.HasPrefix(next, prefix) || strings.ContainsAny(next, "\\\r\n\t ") || strings.Contains(next, "//") {
		return ""
	}
	return next
}

// Apply change le rôle d'une ou de plusieurs personnes (POST
// /{slug}/admin/roles). Avant le tirage l'écriture est directe ; après, elle
// passe par rulechange : un seul aperçu en comptes, puis réparation minimale
// ou conservation pour les futurs tirages.
func (p *Pages) Apply() http.Handler {
	return websec.RequireUnsafeMethod(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		g, ok := p.admin(w, r)
		if !ok {
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, maxFormBytes)
		if err := r.ParseForm(); err != nil {
			p.fail(w, r, g, http.StatusBadRequest, errForm)
			return
		}
		role, ok := ParseRole(r.PostForm.Get(formRole))
		if !ok {
			p.fail(w, r, g, http.StatusBadRequest, errInvalidRole)
			return
		}
		ids, ok := parseIDs(r.PostForm[formIDs])
		if !ok {
			p.fail(w, r, g, http.StatusBadRequest, errUnknown)
			return
		}
		if len(ids) == 0 {
			p.fail(w, r, g, http.StatusBadRequest, errNoSelection)
			return
		}
		list, err := p.o.People.List(r.Context(), participants.AdminActor(g.ID), g.ID)
		if err != nil {
			p.fail(w, r, g, http.StatusInternalServerError, errInternal)
			return
		}
		current := map[int64]participants.DrawRole{}
		for _, pt := range list {
			current[pt.ID] = pt.Role
		}
		for _, id := range ids {
			if _, in := current[id]; !in {
				p.fail(w, r, g, http.StatusNotFound, errUnknown)
				return
			}
		}
		choice := rulechange.ChoiceAsk
		switch r.PostForm.Get(formChoice) {
		case choiceRepair:
			choice = rulechange.ChoiceRepair
		case choiceFuture:
			choice = rulechange.ChoiceFutureOnly
		}
		var applied, future, repaired int
		var pending confirm
		pending.Role, pending.RoleKey = string(role), "admin.roles.role."+string(role)
		for _, id := range ids {
			if current[id] == role {
				continue
			}
			out, err := p.o.Roles.SetRole(r.Context(), g.ID, id, role, choice)
			if err != nil {
				slog.Warn("roles : changement refusé", "err", err)
				status, key := http.StatusInternalServerError, errInternal
				if errs.Is(err, errs.KindValidation) {
					status, key = http.StatusBadRequest, errInvalidRole
				}
				p.fail(w, r, g, status, key)
				return
			}
			switch out.Status {
			case rulechange.StatusNeedsConfirmation:
				pending.IDs = append(pending.IDs, id)
				pending.Changes++
				pending.Affected += out.Preview.AffectedCount
				pending.Discovered += out.Preview.AlreadyDiscoveredCount
			case rulechange.StatusSavedFutureOnly:
				future++
			default:
				applied++
				if out.Repaired {
					repaired++
				}
			}
		}
		if applied+future > 0 && p.o.Journal != nil {
			p.o.Journal(r.Context(), g.ID, ActionRoleSet, map[string]int{"changes": applied + future, "repaired": repaired, "future_only": future})
		}
		if len(pending.IDs) > 0 {
			v, _, err := p.build(r.Context(), r, g)
			if err != nil {
				http.Error(w, "Internal error", http.StatusInternalServerError)
				return
			}
			v.Confirm = &pending
			v.NoticeApplied, v.NoticeFuture, v.NoticeRepaired = applied, future, repaired
			p.render(w, r, http.StatusOK, v)
			return
		}
		if next := safeNext(g.Slug, r.PostForm.Get(formNext)); next != "" {
			http.Redirect(w, r, next, http.StatusSeeOther)
			return
		}
		qs := url.Values{"applied": {strconv.Itoa(applied)}, "future": {strconv.Itoa(future)}, "repaired": {strconv.Itoa(repaired)}}
		http.Redirect(w, r, "/"+g.Slug+"/admin/roles?"+qs.Encode(), http.StatusSeeOther)
	}))
}

func parseIDs(raw []string) ([]int64, bool) {
	seen := map[int64]bool{}
	var out []int64
	if len(raw) > maxSelection {
		return nil, false
	}
	for _, s := range raw {
		id, err := strconv.ParseInt(s, 10, 64)
		if err != nil || id <= 0 {
			return nil, false
		}
		if !seen[id] {
			seen[id] = true
			out = append(out, id)
		}
	}
	return out, true
}

// Route est une route à brancher.
type Route struct {
	Pattern string
	Handler http.Handler
	// Modifies est vrai pour les routes qui changent l'état (déjà enveloppées
	// par websec.RequireUnsafeMethod).
	Modifies bool
}

// Routes liste les routes du panneau.
func (p *Pages) Routes() []Route {
	return []Route{
		{"GET /{slug}/admin/roles", p.Panel(), false},
		{"POST /{slug}/admin/roles", p.Apply(), true},
	}
}

// Register enregistre les routes sur le multiplexeur.
func (p *Pages) Register(mux *http.ServeMux) {
	for _, r := range p.Routes() {
		mux.Handle(r.Pattern, r.Handler)
	}
}
