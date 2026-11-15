// Package public porte les pages publiques de l'inscription par lien (F11c.1) :
// formulaire (screen-inscription), saisie du code (screen-inscription-code),
// lien de confirmation en un clic et écran de réussite
// (screen-inscription-reussie). Les règles d'inscription vivent dans
// internal/registration ; ce paquet ne fait que les afficher.
//
// Règles tenues : tout gestionnaire qui modifie l'état est enveloppé par
// websec.RequireUnsafeMethod (un GET ne modifie jamais rien), le lien de
// confirmation affiche un bouton sur GET et ne consomme qu'au POST, aucune
// page ne contient de style, de script ni de ressource data: en ligne, tous
// les textes passent par les catalogues (module registration, clés
// registration.public.*), la langue du formulaire est enregistrée comme
// locale du participant, et le budget n'est rappelé que s'il est défini.
package public

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"html/template"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"time"

	"secretsanta/internal/access/limiter"
	"secretsanta/internal/access/magic"
	"secretsanta/internal/groups"
	"secretsanta/internal/i18n/catalog"
	"secretsanta/internal/i18n/negotiate"
	i18nweb "secretsanta/internal/i18n/web"
	"secretsanta/internal/registration/code"
	"secretsanta/internal/registration/invite"
	"secretsanta/internal/registration/verify"
	"secretsanta/internal/shared/errs"
	"secretsanta/internal/websec"
	"secretsanta/web"
)

// Invites est la part du service d'invitation (F11.1) utilisée ici ;
// *invite.Service la satisfait.
type Invites interface {
	Open(ctx context.Context, slug, token string) (*invite.Invite, error)
	Submit(ctx context.Context, slug, token string, sub invite.Submission) (*invite.Result, error)
	VerificationAllowed(ctx context.Context, groupID int64) error
}

// Verifier est la part du service de vérification (F11.4) utilisée ici ;
// *verify.Service la satisfait.
type Verifier interface {
	VerifyCode(ctx context.Context, groupID int64, ref, input string) (*verify.Success, error)
	PreviewLink(ctx context.Context, groupID int64, tok string) (code.Preview, error)
	ConfirmLink(ctx context.Context, groupID int64, tok string) (*verify.Success, error)
}

// Groups résout un groupe par son adresse ; *groups.Service le satisfait.
type Groups interface {
	BySlug(ctx context.Context, slug string) (*groups.Group, error)
}

// Options rassemble les dépendances.
type Options struct {
	Groups     Groups
	Invites    Invites
	Verify     Verifier
	Translator i18nweb.Translator

	// Facultatifs.
	Neutral *magic.Neutral
	Logger  *slog.Logger
	// ClientIP renvoie l'adresse du client (défaut : hôte de RemoteAddr).
	ClientIP func(*http.Request) string
	// LetterPath donne la page d'écriture de la lettre (F5c) sans son
	// paramètre bienvenue (défaut : /<slug>/lettre). Home donne l'espace de
	// la personne (défaut : /<slug>/participant).
	LetterPath func(slug string) string
	Home       func(slug string) string
	// Location sert à écrire la date de révélation (défaut : UTC).
	Location *time.Location
}

// Pages construit les gestionnaires.
type Pages struct {
	o   Options
	tpl map[string]*template.Template
}

// Noms des gabarits (web/templates/registration/public/<nom>.html).
const (
	tplForm    = "form"
	tplCode    = "code"
	tplConfirm = "confirm"
	tplSuccess = "success"
	tplError   = "error"
)

// Routes du module (sous l'adresse du groupe). Le formulaire vit sous
// magic.PatternJoin et magic.PatternJoinBare (voir Join).
const (
	RouteCode    = "/{slug}/inscription/code"
	RouteConfirm = "/{slug}/inscription/confirmer/{token}"
)

const (
	maxFormBytes = 4 << 10
	fieldRef     = "ref"
	fieldCode    = "code"
	fieldBack    = "retour"
)

// New analyse les gabarits (mise en page et sélecteur de langue compris).
func New(o Options) (*Pages, error) {
	if o.Groups == nil || o.Invites == nil || o.Verify == nil || o.Translator == nil {
		return nil, errors.New("registrationpages/public: dépendance manquante")
	}
	if o.Logger == nil {
		o.Logger = slog.Default()
	}
	if o.Neutral == nil {
		o.Neutral = magic.NewNeutral(nil)
	}
	if o.ClientIP == nil {
		o.ClientIP = remoteHost
	}
	if o.LetterPath == nil {
		o.LetterPath = func(s string) string { return "/" + s + "/lettre" }
	}
	if o.Home == nil {
		o.Home = func(s string) string { return "/" + s + "/participant" }
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
	p := &Pages{o: o, tpl: map[string]*template.Template{}}
	// Chaque page définit son propre bloc "layout/content" : un jeu de
	// gabarits par page.
	for _, name := range []string{tplForm, tplCode, tplConfirm, tplSuccess, tplError} {
		t, err := template.New("").Funcs(funcs).ParseFS(web.Templates,
			"templates/layout/*.html", "templates/i18n/*.html", "templates/registration/public/"+name+".html")
		if err != nil {
			return nil, fmt.Errorf("registrationpages/public: gabarit %s : %w", name, err)
		}
		p.tpl[name] = t
	}
	return p, nil
}

func remoteHost(r *http.Request) string {
	h, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return h
}

// page est la donnée commune des gabarits.
type page struct {
	Lang      string
	TitleKey  string
	Slug      string
	GroupName string
	// Action est l'adresse où le formulaire envoie son POST.
	Action string
	// Back est l'adresse du formulaire (« Modifier mon adresse »), vide si
	// inconnue.
	Back string
	Ref  string
	// CodeLen est la longueur du code de vérification.
	CodeLen int
	// Code est le code « déjà saisi » de la page de confirmation.
	Code string

	// Formulaire.
	Closed     string // clé du message de fermeture, vide si ouvert
	MessageKey string // clé du message général
	Values     invite.Values
	FirstErr   string // clés des messages par champ
	NameErr    string
	EmailErr   string
	NameTaken  bool
	LoginPath  string // « Recevoir mon lien »
	AskShared  bool
	SharedOn   bool

	// Code, erreur.
	ErrorKey string

	// Réussite.
	Success *success
}

// success est ce que l'écran de réussite affiche.
type success struct {
	FirstName string
	// Budget est vide quand aucun budget n'est défini : rien n'est alors
	// affiché.
	Budget string
	// RevealDate est vide quand la date de révélation n'est pas définie.
	RevealDate  string
	LetterHref  string
	LaterHref   string
	Already     bool
	HasSchedule bool
}

func (p *Pages) render(w http.ResponseWriter, r *http.Request, status int, name string, d page) {
	clone, err := p.tpl[name].Clone()
	if err != nil {
		http.Error(w, "Erreur interne.", http.StatusInternalServerError)
		return
	}
	clone.Funcs(websec.TemplateFuncs(r)).Funcs(i18nweb.Funcs(p.o.Translator, r))
	d.Lang = negotiate.Lang(r.Context(), p.o.Translator.DefaultLang())
	var buf bytes.Buffer
	if err := clone.ExecuteTemplate(&buf, "layout/base", d); err != nil {
		p.o.Logger.Error("inscription : rendu", "page", name, "erreur", err)
		http.Error(w, "Erreur interne.", http.StatusInternalServerError)
		return
	}
	h := w.Header()
	h.Set("Content-Type", "text/html; charset=utf-8")
	// Le jeton d'invitation est dans l'adresse : rien n'est mis en cache ni
	// transmis en référent.
	h.Set("Cache-Control", "no-store")
	h.Set("Referrer-Policy", "no-referrer")
	w.WriteHeader(status)
	_, _ = w.Write(buf.Bytes())
}

// ---------------------------------------------------------------------------
// Formulaire

// Join renvoie le gestionnaire de magic.PatternJoin et magic.PatternJoinBare :
// la réponse neutre est celle de magic.InvitationPaths ; un jeton juste
// affiche le formulaire (GET) ou traite la soumission (POST).
func (p *Pages) Join() http.Handler {
	paths := &magic.InvitationPaths{Forms: joinForms{p}, Neutral: p.o.Neutral, Logger: p.o.Logger}
	inner := paths.Join()
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		noReferrer(w)
		inner.ServeHTTP(w, r)
	})
}

type joinForms struct{ p *Pages }

var _ magic.InviteForms = joinForms{}

// InviteForm implémente magic.InviteForms : nil pour un slug inconnu, un mode
// désactivé ou un jeton faux.
func (f joinForms) InviteForm(ctx context.Context, slug, token string) (http.Handler, error) {
	p := f.p
	inv, err := p.o.Invites.Open(ctx, slug, token)
	if errors.Is(err, invite.ErrNeutral) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	submit := websec.RequireUnsafeMethod(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p.submit(w, r, inv, slug, token)
	}))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		noReferrer(w)
		switch r.Method {
		case http.MethodGet, http.MethodHead:
			p.renderForm(w, r, http.StatusOK, inv, nil)
		case http.MethodPost:
			submit.ServeHTTP(w, r)
		default:
			w.Header().Set("Allow", "GET, HEAD, POST")
			http.Error(w, "Méthode non autorisée.", http.StatusMethodNotAllowed)
		}
	}), nil
}

func noReferrer(w http.ResponseWriter) {
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("Cache-Control", "no-store")
}

func (p *Pages) submit(w http.ResponseWriter, r *http.Request, inv *invite.Invite, slug, token string) {
	r.Body = http.MaxBytesReader(w, r.Body, maxFormBytes)
	if err := r.ParseForm(); err != nil {
		p.renderError(w, r, http.StatusBadRequest, "registration.public.error.bad_request")
		return
	}
	sub := invite.Submission{
		FirstName:     r.PostFormValue(invite.FieldFirstName),
		Name:          r.PostFormValue(invite.FieldName),
		Email:         r.PostFormValue(invite.FieldEmail),
		Honeypot:      r.PostFormValue(invite.FieldHoneypot),
		ConfirmShared: r.PostFormValue(invite.FieldConfirmShared) == "1",
		IP:            p.o.ClientIP(r),
		// La langue choisie (sélecteur de F13) devient la locale du
		// participant ; le service écarte une valeur invalide.
		Lang: negotiate.Lang(r.Context(), ""),
	}
	res, err := p.o.Invites.Submit(r.Context(), slug, token, sub)
	switch {
	case errors.Is(err, invite.ErrNeutral):
		p.o.Neutral.ServeHTTP(w, r)
		return
	case errors.Is(err, limiter.ErrLimited):
		p.renderError(w, r, http.StatusTooManyRequests, "registration.error.limited")
		return
	case err != nil:
		p.o.Logger.Error("inscription", "erreur", err)
		p.renderError(w, r, http.StatusInternalServerError, "registration.error.internal")
		return
	}
	status := http.StatusOK
	if res.Outcome == invite.OutcomeInvalid || res.Outcome == invite.OutcomeNameTaken {
		status = http.StatusUnprocessableEntity
	}
	shown := *inv
	shown.Closed = res.Closed
	p.renderForm(w, r, status, &shown, res)
}

// renderForm rend le formulaire, son refus de champs, la fermeture ou, une
// fois le code envoyé, la saisie du code. Il ne montre jamais le nom ni
// l'adresse d'une autre inscription.
func (p *Pages) renderForm(w http.ResponseWriter, r *http.Request, status int, inv *invite.Invite, res *invite.Result) {
	d := page{TitleKey: "registration.title.form", Slug: inv.Slug, GroupName: inv.Name, Action: r.URL.Path, CodeLen: code.Length}
	if inv.Closed != "" {
		key, _ := inv.Closed.Message()
		d.Closed = key
		p.render(w, r, status, tplForm, d)
		return
	}
	if res != nil {
		switch {
		case res.Outcome == invite.OutcomeClosed && res.Closed != "":
			key, _ := res.Closed.Message()
			d.Closed = key
		case res.Outcome == invite.OutcomeCodeSent:
			d.TitleKey = "registration.title.code"
			d.Ref = res.Ref
			d.Back = r.URL.Path
			d.Action = "/" + inv.Slug + "/inscription/code"
			p.render(w, r, status, tplCode, d)
			return
		default:
			d.Values = res.Values
			d.FirstErr = res.Fields[invite.FieldFirstName].Code
			d.NameErr = res.Fields[invite.FieldName].Code
			d.EmailErr = res.Fields[invite.FieldEmail].Code
			d.NameTaken = res.Outcome == invite.OutcomeNameTaken
			if d.NameTaken {
				d.LoginPath = "/" + inv.Slug + "/connexion"
			}
			d.AskShared = res.Outcome == invite.OutcomeConfirmShared
		}
	}
	p.render(w, r, status, tplForm, d)
}

// ---------------------------------------------------------------------------
// Code

// CodeSubmit sert POST /{slug}/inscription/code : vérifie le code saisi pour
// l'inscription de référence « ref ».
func (p *Pages) CodeSubmit() http.Handler {
	return websec.RequireUnsafeMethod(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		noReferrer(w)
		g, ok := p.group(r)
		if !ok {
			p.refused(w, r)
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, maxFormBytes)
		if err := r.ParseForm(); err != nil {
			p.renderError(w, r, http.StatusBadRequest, "registration.public.error.bad_request")
			return
		}
		ref, input := r.PostFormValue(fieldRef), r.PostFormValue(fieldCode)
		if err := p.o.Invites.VerificationAllowed(r.Context(), g.ID); err != nil {
			p.fail(w, r, err)
			return
		}
		s, err := p.o.Verify.VerifyCode(r.Context(), g.ID, ref, input)
		if code.IsRefused(err) {
			p.render(w, r, http.StatusUnprocessableEntity, tplCode, page{
				TitleKey: "registration.title.code", Slug: g.Slug, GroupName: g.Name,
				Action: r.URL.Path, Ref: ref, CodeLen: code.Length,
				Back:     safeBack(g.Slug, r.PostFormValue(fieldBack)),
				ErrorKey: "registration.code.refused",
			})
			return
		}
		if err != nil {
			p.fail(w, r, err)
			return
		}
		p.success(w, r, g, s)
	}))
}

// safeBack n'accepte comme retour que le chemin d'un formulaire du même
// groupe : jamais d'adresse libre.
func safeBack(slug, v string) string {
	prefix := "/" + slug + "/rejoindre/"
	if !strings.HasPrefix(v, prefix) || len(v) > 512 || strings.ContainsAny(v, "\\\r\n \t?#") || strings.Contains(v, "//") {
		return ""
	}
	return v
}

// ---------------------------------------------------------------------------
// Lien de confirmation en un clic

// ConfirmPage sert GET /{slug}/inscription/confirmer/{token} : le code déjà
// saisi et un bouton, sans rien modifier (un scanneur de courriel qui
// précharge le lien n'a aucun effet).
func (p *Pages) ConfirmPage() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		noReferrer(w)
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			http.Error(w, "Méthode non autorisée.", http.StatusMethodNotAllowed)
			return
		}
		g, ok := p.group(r)
		if !ok {
			p.refused(w, r)
			return
		}
		if err := p.o.Invites.VerificationAllowed(r.Context(), g.ID); err != nil {
			p.fail(w, r, err)
			return
		}
		pv, err := p.o.Verify.PreviewLink(r.Context(), g.ID, r.PathValue("token"))
		if err != nil {
			p.fail(w, r, err)
			return
		}
		p.render(w, r, http.StatusOK, tplConfirm, page{
			TitleKey: "registration.title.confirm", Slug: g.Slug, GroupName: g.Name,
			Action: r.URL.Path, Code: pv.Code,
		})
	})
}

// ConfirmSubmit sert POST /{slug}/inscription/confirmer/{token} : consomme le
// jeton et vérifie l'inscription.
func (p *Pages) ConfirmSubmit() http.Handler {
	return websec.RequireUnsafeMethod(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		noReferrer(w)
		g, ok := p.group(r)
		if !ok {
			p.refused(w, r)
			return
		}
		if err := p.o.Invites.VerificationAllowed(r.Context(), g.ID); err != nil {
			p.fail(w, r, err)
			return
		}
		s, err := p.o.Verify.ConfirmLink(r.Context(), g.ID, r.PathValue("token"))
		if err != nil {
			p.fail(w, r, err)
			return
		}
		p.success(w, r, g, s)
	}))
}

// ---------------------------------------------------------------------------
// Réussite et erreurs

// success rend l'écran de réussite : aucun secret, aucune paire. Le budget et
// la date de révélation ne sont écrits que s'ils sont définis.
func (p *Pages) success(w http.ResponseWriter, r *http.Request, g *groups.Group, s *verify.Success) {
	d := page{TitleKey: "registration.title.done", Slug: g.Slug, GroupName: g.Name}
	su := &success{
		FirstName:  s.FirstName,
		Budget:     g.BudgetLabel(),
		LetterHref: p.o.LetterPath(g.Slug) + "?bienvenue=1",
		LaterHref:  p.o.Home(g.Slug),
		Already:    s.AlreadyVerified,
	}
	if g.RevealAt != nil {
		su.RevealDate = p.revealDate(r, g.RevealAt.In(p.o.Location))
	}
	su.HasSchedule = su.Budget != "" || su.RevealDate != ""
	d.Success = su
	p.render(w, r, http.StatusOK, tplSuccess, d)
}

// revealDate écrit la date de révélation dans la langue de la requête, à
// partir des textes du catalogue.
func (p *Pages) revealDate(r *http.Request, t time.Time) string {
	lang := negotiate.Lang(r.Context(), p.o.Translator.DefaultLang())
	tr := p.o.Translator
	day := fmt.Sprint(t.Day())
	if t.Day() == 1 {
		day = tr.T(lang, "registration.public.day_first", catalog.Pairs())
	}
	month := tr.T(lang, fmt.Sprintf("registration.public.month.%d", int(t.Month())), catalog.Pairs())
	return tr.T(lang, "registration.public.reveal_when", catalog.Pairs(
		"day", day, "month", month, "time", t.Format("15:04")))
}

func (p *Pages) group(r *http.Request) (*groups.Group, bool) {
	g, err := p.o.Groups.BySlug(r.Context(), r.PathValue("slug"))
	if err != nil || g == nil {
		return nil, false
	}
	return g, true
}

// refused répond le refus uniforme d'un lien ou d'un code : même page pour un
// groupe inconnu, un jeton faux, utilisé ou expiré.
func (p *Pages) refused(w http.ResponseWriter, r *http.Request) {
	p.renderError(w, r, http.StatusNotFound, "registration.error.refused")
}

func (p *Pages) renderError(w http.ResponseWriter, r *http.Request, status int, key string) {
	p.render(w, r, status, tplError, page{TitleKey: "registration.title.error", ErrorKey: key})
}

// closedKeys sont les seuls codes d'erreur de fermeture affichés tels quels.
var closedKeys = map[string]bool{
	"registration.closed_draw": true,
	"registration.closed_full": true,
	"registration.closed_date": true,
}

// fail traduit une erreur de service en page : plafond atteint (429), refus
// uniforme (404), fermeture (403), sinon erreur interne.
func (p *Pages) fail(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, limiter.ErrLimited):
		p.renderError(w, r, http.StatusTooManyRequests, "registration.error.limited")
	case code.IsRefused(err):
		p.refused(w, r)
	case errs.Is(err, errs.KindForbidden):
		key, _ := errs.CodeOf(err)
		if !closedKeys[key] {
			key = "registration.error.refused"
		}
		p.renderError(w, r, http.StatusForbidden, key)
	case errs.Is(err, errs.KindNotFound) || errs.Is(err, errs.KindUnauthorized):
		p.refused(w, r)
	default:
		p.o.Logger.Error("inscription : erreur interne", "erreur", err)
		p.renderError(w, r, http.StatusInternalServerError, "registration.error.internal")
	}
}

// ---------------------------------------------------------------------------
// Routes

// Route décrit une route à enregistrer.
type Route struct {
	Pattern string
	Handler http.Handler
	// Modifies est vrai pour les routes qui changent l'état (déjà enveloppées
	// par websec.RequireUnsafeMethod).
	Modifies bool
}

// Routes liste les routes de ce paquet, formulaire compris (les deux motifs
// de magic.InvitationPaths).
func (p *Pages) Routes() []Route {
	join := p.Join()
	return []Route{
		{magic.PatternJoin, join, false},
		{magic.PatternJoinBare, join, false},
		{"GET " + RouteConfirm, p.ConfirmPage(), false},
		{"POST " + RouteConfirm, p.ConfirmSubmit(), true},
		{"POST " + RouteCode, p.CodeSubmit(), true},
	}
}

// Register enregistre toutes les routes sur le multiplexeur.
func (p *Pages) Register(mux *http.ServeMux) {
	for _, r := range p.Routes() {
		mux.Handle(r.Pattern, r.Handler)
	}
}
