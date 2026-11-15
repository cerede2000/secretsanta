// Package people porte la gestion des participants côté administrateur
// (F10.3) : la page des participants (recherche, filtres, menu « … » par
// ligne, confirmation de suppression intégrée, foyers, exclusions et
// forçages) et la fiche d'une personne (statut d'accès, désinscriptions,
// renvoi du lien, correction d'adresse, régénération du lien, QR code).
//
// Aucune page n'affiche une cible : ni la fiche, ni la liste ne lisent la
// table des paires. Les écritures sensibles exigent la session administrateur
// du groupe, le jeton CSRF (RequireUnsafeMethod) et sont consignées au
// journal. Après le tirage, les changements de règles passent par rulechange
// (aperçu en comptes, jamais de nouveau tirage silencieux).
package people

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"html/template"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"secretsanta/internal/groups"
	"secretsanta/internal/i18n/catalog"
	"secretsanta/internal/i18n/negotiate"
	i18nweb "secretsanta/internal/i18n/web"
	"secretsanta/internal/mail/queue"
	"secretsanta/internal/participants"
	"secretsanta/internal/rulechange"
	"secretsanta/internal/shared/adminnav"
	"secretsanta/internal/shared/clock"
	"secretsanta/internal/shared/errs"
	"secretsanta/internal/websec"
	"secretsanta/web"
)

// Groups est la part du service des groupes utilisée ici.
type Groups interface {
	BySlug(ctx context.Context, slug string) (*groups.Group, error)
}

// People est la part du service des participants (F3) utilisée ici.
type People interface {
	List(ctx context.Context, a participants.Actor, groupID int64) ([]participants.Participant, error)
	Get(ctx context.Context, a participants.Actor, groupID, id int64) (*participants.Participant, error)
	Create(ctx context.Context, a participants.Actor, groupID int64, in participants.Input) (*participants.Participant, error)
	Update(ctx context.Context, a participants.Actor, groupID, id int64, in participants.Input) (*participants.Participant, error)
	Delete(ctx context.Context, a participants.Actor, groupID, id int64, confirmed bool) error
}

// Credentials est la part du service d'accès (F2b) utilisée ici : émettre un
// accès remplace l'ancien, qui cesse de fonctionner.
type Credentials interface {
	IssueLink(ctx context.Context, groupID, participantID string) (string, error)
	IssueCode(ctx context.Context, groupID, participantID string) (string, error)
}

// Mailer est la file de courriels (F6).
type Mailer interface {
	EnqueueMail(ctx context.Context, it queue.Item) (int64, error)
}

// Unsubscribes est la part du service de désinscription (F6.4) utilisée ici.
type Unsubscribes interface {
	IsUnsubscribed(ctx context.Context, participantID int64, mailType string) (bool, error)
}

// Rules est la part du service de règles (F3) utilisée ici.
type Rules interface {
	ForgetParticipant(ctx context.Context, groupID, participantID int64) error
}

// RuleChanges est la part du service rulechange (F3d) utilisée ici.
type RuleChanges interface {
	AddExclusion(ctx context.Context, groupID, giver, receiver int64, choice rulechange.Choice) (rulechange.Outcome, error)
	AddForced(ctx context.Context, groupID, giver, receiver int64, choice rulechange.Choice) (rulechange.Outcome, error)
	CreateHousehold(ctx context.Context, groupID int64, name string, members []int64, choice rulechange.Choice) (rulechange.Outcome, error)
	AddHouseholdMember(ctx context.Context, groupID, householdID, participantID int64, choice rulechange.Choice) (rulechange.Outcome, error)
	RemoveExclusion(ctx context.Context, groupID, giver, receiver int64) error
	RemoveForced(ctx context.Context, groupID, giver, receiver int64) error
	RemoveHouseholdMember(ctx context.Context, groupID, householdID, participantID int64) error
}

// Auditor est le journal des actions sensibles (F10.1).
type Auditor interface {
	Record(ctx context.Context, a participants.Actor, groupID int64, action string, participantID int64) error
}

// DrawState est le port d'état du tirage.
type DrawState interface {
	IsDrawDone(ctx context.Context, groupID int64) (bool, error)
}

// Options rassemble les dépendances.
type Options struct {
	DB          *sql.DB
	Clock       clock.Clock
	Groups      Groups
	People      People
	Credentials Credentials
	Mail        Mailer
	Unsub       Unsubscribes
	Rules       Rules
	Changes     RuleChanges
	Audit       Auditor
	State       DrawState
	Catalog     *catalog.Catalog
	// LinkURL construit le lien personnel absolu (groupe, jeton).
	LinkURL func(slug, token string) string
	// Location sert à écrire les dates ; UTC si nil.
	Location *time.Location
	// Nav donne les entrées de la navigation d'administration ; facultatif.
	Nav func(r *http.Request, activeKey string) []adminnav.Item
}

// Pages construit les gestionnaires.
type Pages struct {
	o   Options
	tpl *template.Template

	mu  sync.Mutex
	qrs map[string]qrEntry
}

// Journal : codes d'action consignés.
const (
	ActionLinkResent  = "lien_renvoye"
	ActionAddressFix  = "adresse_corrigee"
	ActionLinkRegen   = "lien_regenere"
	ActionPersonAdded = "personne_ajoutee"
	ActionPersonEdit  = "personne_modifiee"
	ActionPersonDel   = "personne_supprimee"
	ActionRuleChange  = "regle_modifiee"
)

const (
	navKey       = "participants"
	maxFormBytes = 64 << 10
	qrTTL        = 10 * time.Minute
	maxQRs       = 256
	// mailTypeInvitation est le type de courriel du lien renvoyé.
	mailTypeInvitation = "invitation"
)

// New analyse les gabarits.
func New(o Options) (*Pages, error) {
	if o.DB == nil || o.Clock == nil || o.Groups == nil || o.People == nil || o.Credentials == nil ||
		o.Mail == nil || o.Unsub == nil || o.Rules == nil || o.Changes == nil || o.Audit == nil ||
		o.State == nil || o.Catalog == nil || o.LinkURL == nil {
		return nil, errors.New("people : dépendance manquante")
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
		"templates/layout/*.html", "templates/i18n/*.html",
		"templates/admin/people/*.html", "templates/admin/roles/fragments.html")
	if err != nil {
		return nil, fmt.Errorf("people : gabarits : %w", err)
	}
	return &Pages{o: o, tpl: t, qrs: map[string]qrEntry{}}, nil
}

// Route est une route à brancher.
type Route struct {
	Pattern string
	Handler http.Handler
	// Modifies est vrai pour les routes qui changent l'état (déjà enveloppées
	// par websec.RequireUnsafeMethod).
	Modifies bool
}

// Routes liste les routes. L'orchestrateur les branche derrière la chaîne
// SecurityHeaders, CSRF, Sessions.
func (p *Pages) Routes() []Route {
	b := "/{slug}/admin/participants"
	return []Route{
		{"GET " + b, p.List(), false},
		{"POST " + b, p.Create(), true},
		{"GET " + b + "/{id}", p.Sheet(), false},
		{"GET " + b + "/{id}/modifier", p.EditForm(), false},
		{"POST " + b + "/{id}/modifier", p.Edit(), true},
		{"POST " + b + "/{id}/supprimer", p.Remove(), true},
		{"POST " + b + "/{id}/renvoyer", p.Resend(), true},
		{"POST " + b + "/{id}/adresse", p.CorrectAddress(), true},
		{"POST " + b + "/{id}/regenerer", p.Regenerate(), true},
		{"GET " + b + "/{id}/qr/{key}", p.QR(), false},
		{"POST " + b + "/regles", p.ApplyRule(), true},
		{"POST " + b + "/regles/retirer", p.RemoveRule(), true},
	}
}

// Register enregistre les routes sur le multiplexeur.
func (p *Pages) Register(mux *http.ServeMux) {
	for _, r := range p.Routes() {
		mux.Handle(r.Pattern, r.Handler)
	}
}

// admin vérifie le groupe et la session administrateur de ce groupe.
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

func (p *Pages) base(g *groups.Group) string { return "/" + g.Slug + "/admin/participants" }

func displayName(pt participants.Participant) string {
	return strings.TrimSpace(pt.FirstName + " " + pt.Name)
}

// pathID lit l'identifiant de la personne dans l'adresse.
func pathID(r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	return id, err == nil && id > 0
}

// person charge une personne du groupe (jamais d'un autre).
func (p *Pages) person(ctx context.Context, g *groups.Group, id int64) (*participants.Participant, error) {
	pt, err := p.o.People.Get(ctx, participants.AdminActor(g.ID), g.ID, id)
	if err != nil {
		return nil, err
	}
	if pt == nil || pt.GroupID != g.ID {
		return nil, errs.NotFound("Cette personne n'existe pas dans ce groupe.")
	}
	return pt, nil
}

// errorKey traduit une erreur de service en clé de texte (jamais le message
// français brut, pour que la page anglaise reste en anglais).
func errorKey(err error, fallback string) string {
	switch {
	case errs.Is(err, errs.KindValidation):
		return "admin.people.error.invalid"
	case errs.Is(err, errs.KindConflict):
		return fallback
	case errs.Is(err, errs.KindNotFound):
		return "admin.people.error.not_found"
	case errs.Is(err, errs.KindForbidden), errs.Is(err, errs.KindUnauthorized):
		return "admin.people.error.forbidden"
	}
	return "admin.people.error.internal"
}

func statusOf(err error) int {
	switch {
	case errs.Is(err, errs.KindValidation):
		return http.StatusBadRequest
	case errs.Is(err, errs.KindConflict):
		return http.StatusConflict
	case errs.Is(err, errs.KindNotFound):
		return http.StatusNotFound
	case errs.Is(err, errs.KindForbidden), errs.Is(err, errs.KindUnauthorized):
		return http.StatusForbidden
	}
	return http.StatusInternalServerError
}

func (p *Pages) journal(ctx context.Context, g *groups.Group, action string, pid int64) {
	if err := p.o.Audit.Record(ctx, participants.AdminActor(g.ID), g.ID, action, pid); err != nil {
		slog.Error("people : journal", "action", action, "err", err)
	}
}

// render écrit une page d'administration.
func (p *Pages) render(w http.ResponseWriter, r *http.Request, status int, v *view) {
	clone, err := p.tpl.Clone()
	if err != nil {
		http.Error(w, "Internal error", http.StatusInternalServerError)
		return
	}
	clone.Funcs(websec.TemplateFuncs(r)).Funcs(i18nweb.Funcs(p.o.Catalog, r))
	v.Lang = negotiate.Lang(r.Context(), p.o.Catalog.DefaultLang())
	if p.o.Nav != nil {
		v.Nav = p.o.Nav(r, navKey)
	}
	var buf strings.Builder
	if err := clone.ExecuteTemplate(&buf, "layout/admin", v); err != nil {
		slog.Error("people : rendu", "err", err)
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
