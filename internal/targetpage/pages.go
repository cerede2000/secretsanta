// Package targetpage porte les pages du participant avant et après la
// révélation (F4d) : l'attente avec compte à rebours et la préférence « un
// seul cadeau », la carte de cible sans animation (une ou deux cibles),
// l'accueil d'une personne en réception seule et le budget facultatif avec
// son bandeau « Budget mis à jour ».
//
// Règles de confidentialité : une page ne montre que les cibles de la
// personne connectée, jamais avant l'ouverture de la révélation (la porte est
// celle de draw.MyTargets) et jamais une cible non découverte (la seconde
// cible reste « à découvrir » sans nom). Le paquet ne connaît ni les routes ni
// l'analyse des gabarits : l'orchestrateur fournit un Renderer et monte les
// gestionnaires.
package targetpage

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"secretsanta/internal/draw"
	"secretsanta/internal/groups"
	"secretsanta/internal/i18n/negotiate"
	"secretsanta/internal/mail/templates"
	"secretsanta/internal/participants"
	"secretsanta/internal/shared/clock"
	"secretsanta/internal/shared/errs"
	"secretsanta/internal/websec"
)

// Noms des gabarits (web/templates/draw/*.html) à analyser avec le layout.
const (
	TemplateWait      = "draw/wait"
	TemplateCard      = "draw/card"
	TemplateReception = "draw/reception"
	TemplateError     = "draw/error"
)

// maxFormBytes borne le corps d'un formulaire.
const maxFormBytes = 4 << 10

// GroupSource est ce que les pages demandent au service des groupes
// (satisfait par *groups.Service).
type GroupSource interface {
	BySlug(ctx context.Context, slug string) (*groups.Group, error)
}

// ParticipantSource est ce que les pages demandent au service des
// participants (satisfait par *participants.Service). Get est appelé avec la
// personne elle-même, et avec l'administrateur du groupe pour lire le nom et
// l'indice de sa propre cible ; SetRole avec la personne elle-même : le
// service refuse tout rôle autre que normal ou un_seul, et après le tirage.
type ParticipantSource interface {
	Get(ctx context.Context, a participants.Actor, groupID, id int64) (*participants.Participant, error)
	SetRole(ctx context.Context, a participants.Actor, groupID, id int64, role participants.DrawRole) error
}

// DrawSource est la partie du service du tirage utile aux pages (satisfaite
// par *draw.Service) : les cibles de la personne connectée, rien avant
// l'ouverture de la révélation.
type DrawSource interface {
	MyTargets(ctx context.Context, a participants.Actor, groupID int64) (*draw.MyTargets, error)
}

// BudgetStore mémorise la version du budget vue (satisfait par *Store).
type BudgetStore interface {
	BudgetSeen(ctx context.Context, groupID, participantID int64) (int, error)
	MarkBudgetSeen(ctx context.Context, groupID, participantID int64, version int) error
}

// Renderer écrit la page name avec le statut donné (fourni par
// l'orchestrateur : clone des gabarits, fonctions t, tn et csrfField).
type Renderer func(w http.ResponseWriter, r *http.Request, status int, name string, data any)

// Deps regroupe les dépendances des pages.
type Deps struct {
	Groups       GroupSource
	Participants ParticipantSource
	Draw         DrawSource
	Budget       BudgetStore
	Render       Renderer
	Clock        clock.Clock
	// DefaultLang sert si la langue n'a pas été négociée (« fr » si vide).
	DefaultLang string
	// Location sert à écrire la date de révélation (UTC si nil).
	Location *time.Location
	// Chemins des pages voisines ; les valeurs par défaut sont indiquées.
	// DiscoveryPath : « /<slug>/decouverte » (page de F9c).
	DiscoveryPath func(slug string) string
	// OwnLetterPath : « /<slug>/lettre » (F5c, lettre de la personne).
	OwnLetterPath func(slug string) string
	// MailboxPath : « /<slug>/boite » (F5c).
	MailboxPath func(slug string) string
	// TargetLetterPath : « /<slug>/lettre-recue/<cible> » (F5c) ; cible est
	// l'identifiant de la paire, celui que la page de découverte confirme.
	TargetLetterPath func(slug string, pairID int64) string
	// TargetIdeasPath : « /<slug>/suggestions/<cible> » (F8c).
	TargetIdeasPath func(slug string, pairID int64) string
	// HomePath : « /<slug>/participant », où reviennent les formulaires.
	HomePath func(slug string) string
	// PreferencePath et BudgetSeenPath : adresses des deux formulaires,
	// « /<slug>/preference » et « /<slug>/budget-vu ».
	PreferencePath func(slug string) string
	BudgetSeenPath func(slug string) string
}

// Card est la carte d'une cible. Une cible non découverte n'a ni nom ni lien.
type Card struct {
	N, Total   int
	Discovered bool
	Name       string
	Initial    string
	Hint       string
	LetterURL  string
	IdeasURL   string
}

// Page est la donnée de tous les gabarits du paquet.
type Page struct {
	Lang     string
	TitleKey string
	// ErrorKey est la clé du message d'erreur intégré à la page.
	ErrorKey  string
	Slug      string
	GroupName string
	Year      int

	// Budget est le budget formaté, vide quand il n'est pas défini : rien
	// n'est alors affiché. BannerVersion vaut 0 sans bandeau, sinon la
	// version à marquer comme vue.
	Budget        string
	BannerVersion int
	// Jour J, quand il est défini.
	HasEvent      bool
	EventDay      int
	EventMonthKey string

	// Attente.
	NotDone       bool
	HasCountdown  bool
	OpenDay       int
	OpenMonthKey  string
	OpenTime      string
	Days          int
	Hours         int
	Minutes       int
	UntilRFC3339  string
	NowRFC3339    string
	CanPick       bool
	PrefOn        bool
	PrefLocked    bool
	PrefAction    string
	SeenAction    string
	LetterURL     string
	MailboxURL    string
	DiscoveryURL  string
	Cards         []Card
	TwoTargets    bool
	NextDiscovery bool
}

// Pages construit les gestionnaires.
type Pages struct{ d Deps }

// New valide les dépendances et pose les valeurs par défaut.
func New(d Deps) (*Pages, error) {
	if d.Groups == nil || d.Participants == nil || d.Draw == nil || d.Budget == nil || d.Render == nil || d.Clock == nil {
		return nil, errors.New("targetpage : Groups, Participants, Draw, Budget, Render et Clock sont obligatoires")
	}
	if d.DefaultLang == "" {
		d.DefaultLang = "fr"
	}
	if d.Location == nil {
		d.Location = time.UTC
	}
	slugPath := func(suffix string) func(string) string {
		return func(slug string) string { return "/" + slug + suffix }
	}
	pairPath := func(prefix string) func(string, int64) string {
		return func(slug string, id int64) string { return "/" + slug + prefix + strconv.FormatInt(id, 10) }
	}
	if d.DiscoveryPath == nil {
		d.DiscoveryPath = slugPath("/decouverte")
	}
	if d.OwnLetterPath == nil {
		d.OwnLetterPath = slugPath("/lettre")
	}
	if d.MailboxPath == nil {
		d.MailboxPath = slugPath("/boite")
	}
	if d.TargetLetterPath == nil {
		d.TargetLetterPath = pairPath("/lettre-recue/")
	}
	if d.TargetIdeasPath == nil {
		d.TargetIdeasPath = pairPath("/suggestions/")
	}
	if d.HomePath == nil {
		d.HomePath = slugPath("/participant")
	}
	if d.PreferencePath == nil {
		d.PreferencePath = slugPath("/preference")
	}
	if d.BudgetSeenPath == nil {
		d.BudgetSeenPath = slugPath("/budget-vu")
	}
	return &Pages{d: d}, nil
}

func (p *Pages) lang(r *http.Request) string {
	return negotiate.Lang(r.Context(), p.d.DefaultLang)
}

func noStore(w http.ResponseWriter) { w.Header().Set("Cache-Control", "no-store") }

// Home sert la page du participant (GET, ne modifie rien) : l'accueil de
// réception seule, l'attente de la révélation, ou la carte de cible. Elle se
// monte sur « /{slug}/participant » (vers où la connexion renvoie) et sur
// « /{slug}/ma-cible ». Une cible ouverte mais jamais découverte renvoie vers
// la page de découverte : la carte ne montre que des cibles découvertes.
func (p *Pages) Home() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		noStore(w)
		g, me, ok := p.who(w, r)
		if !ok {
			return
		}
		p.show(w, r, g, me, http.StatusOK, "")
	})
}

// Preference bascule le rôle entre normal et un_seul (POST seulement : un GET
// renvoie 405 sans rien modifier). Le formulaire envoie un_seul=1 pour passer
// à « un seul cadeau », autre chose pour revenir à normal.
func (p *Pages) Preference() http.Handler {
	return websec.RequireUnsafeMethod(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		noStore(w)
		g, me, ok := p.who(w, r)
		if !ok {
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, maxFormBytes)
		if err := r.ParseForm(); err != nil {
			p.show(w, r, g, me, http.StatusBadRequest, "draw.pref.error.generic")
			return
		}
		// Jamais d'accès à reception_seule ni offre_plus par cette page : le
		// service le refuse aussi, mais on ne l'appelle pas.
		if me.Role != participants.RoleNormal && me.Role != participants.RoleUnSeul {
			p.show(w, r, g, me, http.StatusForbidden, "draw.pref.error.generic")
			return
		}
		role := participants.RoleNormal
		if r.PostFormValue("un_seul") == "1" {
			role = participants.RoleUnSeul
		}
		if err := p.d.Participants.SetRole(r.Context(), participants.ParticipantActor(g.ID, me.ID), g.ID, me.ID, role); err != nil {
			status, key := http.StatusInternalServerError, "core.errors.internal"
			switch errs.KindOf(err) {
			case errs.KindConflict:
				status, key = http.StatusConflict, "draw.pref.error.locked"
			case errs.KindForbidden, errs.KindValidation:
				status, key = http.StatusForbidden, "draw.pref.error.generic"
			}
			p.show(w, r, g, me, status, key)
			return
		}
		http.Redirect(w, r, p.d.HomePath(g.Slug), http.StatusSeeOther)
	}))
}

// BudgetSeen retient que la personne a vu la version du budget (POST
// seulement). La version ne dépasse jamais celle du groupe.
func (p *Pages) BudgetSeen() http.Handler {
	return websec.RequireUnsafeMethod(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		noStore(w)
		g, me, ok := p.who(w, r)
		if !ok {
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, maxFormBytes)
		if err := r.ParseForm(); err != nil {
			p.show(w, r, g, me, http.StatusBadRequest, "draw.pref.error.generic")
			return
		}
		v, err := strconv.Atoi(strings.TrimSpace(r.PostFormValue("version")))
		if err != nil || v < 0 {
			p.show(w, r, g, me, http.StatusBadRequest, "draw.pref.error.generic")
			return
		}
		if v > g.BudgetVersion {
			v = g.BudgetVersion
		}
		if err := p.d.Budget.MarkBudgetSeen(r.Context(), g.ID, me.ID, v); err != nil {
			p.show(w, r, g, me, http.StatusInternalServerError, "core.errors.internal")
			return
		}
		http.Redirect(w, r, p.d.HomePath(g.Slug), http.StatusSeeOther)
	}))
}

// who résout le groupe de l'adresse et la personne connectée. Quand ok est
// faux, la réponse est déjà écrite : page neutre 404 (groupe inconnu) ou renvoi
// vers la connexion (sans session valide).
func (p *Pages) who(w http.ResponseWriter, r *http.Request) (*groups.Group, *participants.Participant, bool) {
	g, err := p.d.Groups.BySlug(r.Context(), r.PathValue("slug"))
	if err != nil {
		status, key := http.StatusNotFound, "core.errors.not_found"
		if !errs.Is(err, errs.KindNotFound) {
			status, key = http.StatusInternalServerError, "core.errors.internal"
		}
		p.d.Render(w, r, status, TemplateError, Page{Lang: p.lang(r), TitleKey: "draw.error.title", ErrorKey: key})
		return nil, nil, false
	}
	login := func() {
		http.Redirect(w, r, "/"+g.Slug+"/connexion", http.StatusSeeOther)
	}
	sess, found := websec.ParticipantSession(r.Context(), strconv.FormatInt(g.ID, 10))
	if !found {
		login()
		return nil, nil, false
	}
	pid, err := strconv.ParseInt(sess.Subject, 10, 64)
	if err != nil || pid <= 0 {
		login()
		return nil, nil, false
	}
	me, err := p.d.Participants.Get(r.Context(), participants.ParticipantActor(g.ID, pid), g.ID, pid)
	if err != nil {
		if errs.Is(err, errs.KindNotFound) || errs.Is(err, errs.KindUnauthorized) || errs.Is(err, errs.KindForbidden) {
			login()
			return nil, nil, false
		}
		p.d.Render(w, r, http.StatusInternalServerError, TemplateError,
			Page{Lang: p.lang(r), TitleKey: "draw.error.title", ErrorKey: "core.errors.internal"})
		return nil, nil, false
	}
	return g, me, true
}

// base remplit les champs communs : groupe, budget et bandeau, jour J.
func (p *Pages) base(r *http.Request, g *groups.Group, me *participants.Participant, titleKey string) (Page, error) {
	lang := p.lang(r)
	pg := Page{
		Lang: lang, TitleKey: titleKey, Slug: g.Slug, GroupName: g.Name,
		Year:       p.d.Clock.Now().In(p.d.Location).Year(),
		LetterURL:  p.d.OwnLetterPath(g.Slug),
		MailboxURL: p.d.MailboxPath(g.Slug),
		SeenAction: p.d.BudgetSeenPath(g.Slug),
		PrefAction: p.d.PreferencePath(g.Slug),
	}
	if g.BudgetCents != nil {
		pg.Budget = templates.FormatBudget(lang, *g.BudgetCents)
	}
	if t, err := time.Parse(groups.DateLayout, strings.TrimSpace(g.EventDate)); err == nil {
		pg.HasEvent = true
		pg.EventDay = t.Day()
		pg.EventMonthKey = monthKey(t)
	}
	return pg, nil
}

func monthKey(t time.Time) string { return "draw.month." + strconv.Itoa(int(t.Month())) }

// bannerVersion renvoie la version du budget à signaler, 0 sans bandeau : un
// budget défini dont la version dépasse celle déjà vue. Un budget retiré n'a
// pas de bandeau ; redéfini, sa nouvelle version dépasse la version vue.
func (p *Pages) bannerVersion(ctx context.Context, g *groups.Group, pid int64) (int, error) {
	if g.BudgetCents == nil || g.BudgetVersion <= 0 {
		return 0, nil
	}
	seen, err := p.d.Budget.BudgetSeen(ctx, g.ID, pid)
	if err != nil {
		return 0, err
	}
	if g.BudgetVersion > seen {
		return g.BudgetVersion, nil
	}
	return 0, nil
}

func (p *Pages) fail(w http.ResponseWriter, r *http.Request) {
	p.d.Render(w, r, http.StatusInternalServerError, TemplateError,
		Page{Lang: p.lang(r), TitleKey: "draw.error.title", ErrorKey: "core.errors.internal"})
}

// show choisit et rend la page de la personne. errKey, s'il n'est pas vide,
// est le message d'un formulaire refusé, rendu avec le statut donné.
func (p *Pages) show(w http.ResponseWriter, r *http.Request, g *groups.Group, me *participants.Participant, status int, errKey string) {
	ctx := r.Context()
	if me.Role == participants.RoleReceptionSeule {
		pg, _ := p.base(r, g, me, "draw.reception.title")
		pg.ErrorKey = errKey
		p.d.Render(w, r, status, TemplateReception, pg)
		return
	}
	mine, err := p.d.Draw.MyTargets(ctx, participants.ParticipantActor(g.ID, me.ID), g.ID)
	if err != nil {
		p.fail(w, r)
		return
	}
	banner, err := p.bannerVersion(ctx, g, me.ID)
	if err != nil {
		p.fail(w, r)
		return
	}
	if mine.Ready && len(mine.Targets) == 0 {
		// Rien à offrir : même accueil qu'une personne en réception seule.
		pg, _ := p.base(r, g, me, "draw.reception.title")
		pg.ErrorKey = errKey
		p.d.Render(w, r, status, TemplateReception, pg)
		return
	}
	if !mine.Ready {
		p.wait(w, r, g, me, mine.Done, banner, status, errKey)
		return
	}
	seen := false
	for _, t := range mine.Targets {
		seen = seen || t.Discovered
	}
	if !seen {
		http.Redirect(w, r, p.d.DiscoveryPath(g.Slug), http.StatusSeeOther)
		return
	}
	pg, _ := p.base(r, g, me, "draw.card.title")
	pg.ErrorKey = errKey
	pg.BannerVersion = banner
	pg.TwoTargets = len(mine.Targets) > 1
	pg.DiscoveryURL = p.d.DiscoveryPath(g.Slug)
	for i, t := range mine.Targets {
		c := Card{N: i + 1, Total: len(mine.Targets), Discovered: t.Discovered}
		if t.Discovered {
			rec, err := p.d.Participants.Get(ctx, participants.AdminActor(g.ID), g.ID, t.ReceiverID)
			if err != nil {
				p.fail(w, r)
				return
			}
			c.Name = strings.TrimSpace(rec.FirstName)
			if c.Name == "" {
				c.Name = strings.TrimSpace(rec.Name)
			}
			c.Initial = strings.ToUpper(firstRune(c.Name))
			c.Hint = rec.GiftHint
			c.LetterURL = p.d.TargetLetterPath(g.Slug, t.PairID)
			c.IdeasURL = p.d.TargetIdeasPath(g.Slug, t.PairID)
		}
		pg.Cards = append(pg.Cards, c)
	}
	p.d.Render(w, r, status, TemplateCard, pg)
}

func firstRune(s string) string {
	for _, r := range s {
		return string(r)
	}
	return ""
}

func (p *Pages) wait(w http.ResponseWriter, r *http.Request, g *groups.Group, me *participants.Participant, done bool, banner, status int, errKey string) {
	pg, _ := p.base(r, g, me, "draw.wait.title")
	pg.ErrorKey = errKey
	pg.BannerVersion = banner
	pg.NotDone = !done
	pg.CanPick = me.Role == participants.RoleNormal || me.Role == participants.RoleUnSeul
	pg.PrefOn = me.Role == participants.RoleUnSeul
	pg.PrefLocked = done
	now := p.d.Clock.Now()
	if done && g.RevealAt != nil {
		at := g.RevealAt.In(p.d.Location)
		pg.OpenDay = at.Day()
		pg.OpenMonthKey = monthKey(at)
		pg.OpenTime = at.Format("15:04")
		if left := g.RevealAt.Sub(now); left > 0 {
			pg.HasCountdown = true
			pg.Days = int(left / (24 * time.Hour))
			pg.Hours = int(left%(24*time.Hour)) / int(time.Hour)
			pg.Minutes = int(left%time.Hour) / int(time.Minute)
			pg.UntilRFC3339 = g.RevealAt.UTC().Format(time.RFC3339)
			pg.NowRFC3339 = now.UTC().Format(time.RFC3339)
		}
	}
	p.d.Render(w, r, status, TemplateWait, pg)
}
