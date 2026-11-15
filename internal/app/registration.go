package app

import (
	"context"
	"errors"
	"fmt"
	"html/template"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"secretsanta/internal/access/limiter"
	"secretsanta/internal/access/magic"
	"secretsanta/internal/groups"
	"secretsanta/internal/i18n/negotiate"
	i18nweb "secretsanta/internal/i18n/web"
	"secretsanta/internal/mail/queue"
	"secretsanta/internal/registration/code"
	"secretsanta/internal/registration/invite"
	"secretsanta/internal/registration/sqlstore"
	"secretsanta/internal/registration/verify"
	"secretsanta/internal/shared/errs"
	"secretsanta/internal/websec"
	"secretsanta/web"
)

// Chemins de l'inscription par lien (sous l'adresse du groupe). Le formulaire
// vit sous magic.PatternJoin et magic.PatternJoinBare.
const (
	registrationConfirmPath = "/{slug}/inscription/confirmer/{token}"
	// registrationCodePath reçoit la saisie du code (page minimale, en
	// attendant F11c).
	registrationCodePath = "/{slug}/inscription/code"
	// maxRegistrationFormBytes borne le corps de la saisie du code.
	maxRegistrationFormBytes = 4 << 10
)

// mailEnqueuer adapte la file d'envoi à code.Enqueuer et verify.Enqueuer : sans
// transport (la file ne tourne pas), il refuse net au lieu de déréférencer une
// file absente.
type mailEnqueuer struct{ q *queue.Service }

// EnqueueMail met le courriel en file, ou refuse si l'envoi est désactivé.
func (m mailEnqueuer) EnqueueMail(ctx context.Context, it queue.Item) (int64, error) {
	if m.q == nil {
		return 0, ErrMailDisabled
	}
	return m.q.EnqueueMail(ctx, it)
}

// registrationThrottle adapte les plafonds de code.Service au port
// invite.Throttle : 20 soumissions et 5 créations par fenêtre de 15 minutes et
// par adresse IP et par groupe.
type registrationThrottle struct{ svc *code.Service }

// AllowSubmission implémente invite.Throttle.
func (t registrationThrottle) AllowSubmission(_ context.Context, groupID int64, ip string) error {
	return t.svc.AllowSubmission(groupID, ip)
}

// AllowRegistration implémente invite.Throttle.
func (t registrationThrottle) AllowRegistration(_ context.Context, groupID int64, ip, _ string) error {
	return t.svc.AllowRegistration(groupID, ip)
}

var _ invite.Throttle = registrationThrottle{}

// registerRegistration construit l'inscription par lien : magasins SQL, codes
// de vérification, vérification (qui tient aussi le port RegistrationState du
// tirage : compteur réel des inscriptions en attente, hors ignorées),
// formulaire. Elle précède le tirage, qui en a besoin, et suit les jetons
// d'accès, l'enrôlement et la file d'envoi. Les routes sont montées plus loin
// (registerRegistrationPages). La purge à 48 heures est planifiée ici.
func registerRegistration(a *App) error {
	key := []byte(a.Cfg.SigningKey.Reveal())
	base := a.publicBase()
	enq := mailEnqueuer{q: a.mail.q}
	pending := sqlstore.NewInviteStore(a.DB)
	state := sqlstore.NewVerifyStore(a.DB)

	codes, err := code.New(code.Options{
		Store:  sqlstore.NewCodeStore(a.DB),
		Clock:  a.Clock,
		Mailer: code.TemplateMailer{Catalog: a.Cat, Queue: enq},
		Link:   func(slug, tok string) string { return base + "/" + slug + "/inscription/confirmer/" + tok },
		Key:    key,
	})
	if err != nil {
		return fmt.Errorf("codes de vérification : %w", err)
	}
	ver, err := verify.New(verify.Config{
		Pending: pending, Store: state, Codes: codes, Enrol: a.Enrolment, Groups: a.Groups,
		Draw: a.DrawState, Clock: a.Clock,
		Welcome:   verify.TemplateMailer{Catalog: a.Cat, Queue: enq},
		Scheduler: a.sched, Lang: a.GroupSettings, Logger: slog.Default(),
	})
	if err != nil {
		return fmt.Errorf("vérification des inscriptions : %w", err)
	}
	inv, err := invite.New(invite.Config{
		Groups: a.Groups, Directory: invite.SQLDirectory{DB: a.DB}, Store: pending, Draw: a.DrawState,
		Clock: a.Clock, Key: key, Codes: ver, Throttle: registrationThrottle{svc: codes}, Logger: slog.Default(),
	})
	if err != nil {
		return fmt.Errorf("inscription par lien : %w", err)
	}
	a.Invite, a.RegCodes, a.Verify = inv, codes, ver
	a.regPending, a.regState = pending, state

	// Purge des inscriptions expirées (48 heures) : au démarrage, à
	// l'expiration de chacune, puis périodiquement en filet.
	a.Every("registration.purge", purgeInterval, func(ctx context.Context) error {
		return a.purgeRegistrations(ctx)
	})
	a.OnStart(func(ctx context.Context) {
		if err := a.restoreRegistrations(ctx); err != nil && !errors.Is(err, context.Canceled) {
			slog.Error("reprise de la purge des inscriptions en échec", "erreur", err)
		}
	})
	return nil
}

// purgeRegistrations retire les inscriptions expirées de tous les groupes qui
// en portent, avec leur code, leur état « ignorée » et leur trace.
func (a *App) purgeRegistrations(ctx context.Context) error {
	ids, err := a.regState.GroupIDs(ctx)
	if err != nil {
		return err
	}
	var first error
	for _, id := range ids {
		if _, err := a.Verify.PurgeExpired(ctx, id); err != nil && first == nil {
			first = err
		}
	}
	return first
}

// restoreRegistrations reprend la purge après un redémarrage : les tâches
// planifiées en mémoire ont été perdues. Il purge ce qui est déjà expiré, puis
// replanifie, pour chaque inscription encore vivante, une purge à son
// expiration.
func (a *App) restoreRegistrations(ctx context.Context) error {
	if err := a.purgeRegistrations(ctx); err != nil {
		return err
	}
	ids, err := a.regState.GroupIDs(ctx)
	if err != nil {
		return err
	}
	now := a.Clock.Now()
	for _, id := range ids {
		// Instant zéro : toutes les inscriptions du groupe.
		ps, err := a.regPending.ListPending(ctx, id, time.Time{})
		if err != nil {
			return err
		}
		for _, p := range ps {
			if !p.ExpiresAt.After(now) {
				continue
			}
			gid := p.GroupID
			a.sched.At(p.ExpiresAt, func(ctx context.Context) error {
				_, err := a.Verify.PurgeExpired(ctx, gid)
				return err
			})
		}
	}
	return nil
}

// registrationPages porte les pages de l'inscription : le formulaire
// (invite.Renderer), la saisie du code, le lien de confirmation en un clic et
// l'écran de réussite. Ce sont des pages MINIMALES : F11c les remplacera.
type registrationPages struct {
	a     *App
	forms map[string]*template.Template
}

// Gabarits (web/templates/registration/<nom>.html). Chacun définit son propre
// bloc layout/content : un jeu analysé par page, jamais partagé.
const (
	regTplForm    = "form"
	regTplCode    = "code"
	regTplConfirm = "confirm"
	regTplDone    = "done"
	regTplError   = "error"
)

func newRegistrationPages(a *App) (*registrationPages, error) {
	funcs := template.FuncMap{}
	for k, v := range websec.TemplateFuncsPlaceholder() {
		funcs[k] = v
	}
	for k, v := range i18nweb.FuncsPlaceholder() {
		funcs[k] = v
	}
	p := &registrationPages{a: a, forms: map[string]*template.Template{}}
	for _, name := range []string{regTplForm, regTplCode, regTplConfirm, regTplDone, regTplError} {
		t, err := template.New("").Funcs(funcs).ParseFS(web.Templates,
			"templates/layout/*.html", "templates/i18n/*.html", "templates/registration/"+name+".html")
		if err != nil {
			return nil, fmt.Errorf("gabarit d'inscription %s : %w", name, err)
		}
		p.forms[name] = t
	}
	return p, nil
}

// regPage est la donnée commune des gabarits d'inscription.
type regPage struct {
	Lang      string
	TitleKey  string
	Slug      string
	GroupName string
	Action    string
	Ref       string
	Code      string
	FirstName string

	// Closed est la clé du message de fermeture ; vide si ouvert.
	Closed       string
	MessageKey   string
	ErrorKey     string
	HintKey      string
	FirstNameErr string
	NameErr      string
	EmailErr     string
	AskShared    bool
	Values       invite.Values
	Offers       []regOffer

	AlreadyVerified bool
}

type regOffer struct {
	Key  string
	Href string
}

func (p *registrationPages) lang(r *http.Request) string {
	return negotiate.Lang(r.Context(), p.a.Cfg.DefaultLang)
}

func (p *registrationPages) render(w http.ResponseWriter, r *http.Request, status int, name string, d regPage) {
	clone, err := p.forms[name].Clone()
	if err != nil {
		http.Error(w, "Erreur interne.", http.StatusInternalServerError)
		return
	}
	clone.Funcs(websec.TemplateFuncs(r)).Funcs(i18nweb.Funcs(p.a.Cat, r))
	d.Lang = p.lang(r)
	var buf strings.Builder
	if err := clone.ExecuteTemplate(&buf, "layout/base", d); err != nil {
		http.Error(w, "Erreur interne.", http.StatusInternalServerError)
		return
	}
	h := w.Header()
	h.Set("Content-Type", "text/html; charset=utf-8")
	h.Set("Cache-Control", "no-store")
	h.Set("Referrer-Policy", "no-referrer")
	w.WriteHeader(status)
	_, _ = w.Write([]byte(buf.String()))
}

// Render implémente invite.Renderer : le formulaire, son refus de champs, la
// fermeture ou, une fois le code envoyé, la saisie du code.
func (p *registrationPages) Render(w http.ResponseWriter, r *http.Request, status int, v invite.View) {
	d := regPage{TitleKey: "registration.title.form", Slug: v.Invite.Slug, GroupName: v.Invite.Name, Action: v.Action}
	switch res := v.Result; {
	case v.Closed != "":
		key, _ := v.Closed.Message()
		d.Closed = key
	case res != nil && res.Outcome == invite.OutcomeCodeSent:
		d.TitleKey = "registration.title.code"
		d.Ref = res.Ref
		d.Action = "/" + v.Invite.Slug + "/inscription/code"
		p.render(w, r, status, regTplCode, d)
		return
	case res != nil:
		d.Values = res.Values
		d.MessageKey = res.Problem.Code
		d.FirstNameErr = res.Fields[invite.FieldFirstName].Code
		d.NameErr = res.Fields[invite.FieldName].Code
		d.EmailErr = res.Fields[invite.FieldEmail].Code
		if d.FirstNameErr != "" || d.NameErr != "" || d.EmailErr != "" {
			// Le message de champ suffit ; « Vérifie les champs » reste pour
			// les saisies invalides.
			if res.Outcome == invite.OutcomeNameTaken {
				d.MessageKey = ""
			}
		}
		d.AskShared = res.Outcome == invite.OutcomeConfirmShared
		for _, o := range res.Offers {
			switch {
			case o.Path != "":
				d.Offers = append(d.Offers, regOffer{Key: o.Code, Href: o.Path})
			case o.Kind == invite.OfferPrecision:
				d.HintKey = o.Code
			}
		}
	}
	p.render(w, r, status, regTplForm, d)
}

var _ invite.Renderer = (*registrationPages)(nil)

// group résout le groupe de l'adresse ; faux si inconnu (la réponse est alors
// écrite : la page de refus uniforme).
func (p *registrationPages) group(w http.ResponseWriter, r *http.Request) (*groups.Group, bool) {
	g, err := p.a.Groups.BySlug(r.Context(), r.PathValue("slug"))
	if err != nil || g == nil {
		if err != nil && !errs.Is(err, errs.KindNotFound) {
			p.fail(w, r, err)
			return nil, false
		}
		p.refused(w, r)
		return nil, false
	}
	return g, true
}

// refused répond le refus uniforme d'un lien ou d'un code : même page pour un
// groupe inconnu, un jeton faux, utilisé, expiré ou une inscription ignorée.
func (p *registrationPages) refused(w http.ResponseWriter, r *http.Request) {
	p.render(w, r, http.StatusNotFound, regTplError, regPage{
		TitleKey: "registration.title.error", ErrorKey: "registration.error.refused"})
}

// fail traduit une erreur de service en page : plafond atteint (429), tirage
// fait (403, texte de fermeture), refus uniforme (404), sinon erreur interne.
func (p *registrationPages) fail(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, limiter.ErrLimited):
		p.render(w, r, http.StatusTooManyRequests, regTplError, regPage{
			TitleKey: "registration.title.error", ErrorKey: "registration.error.limited"})
	case code.IsRefused(err):
		p.refused(w, r)
	case errs.Is(err, errs.KindForbidden) || errs.Is(err, errs.KindNotFound):
		key, _ := errs.CodeOf(err)
		if _, known := p.a.Cat.Messages(p.lang(r))[key]; !known {
			key = errs.KindKey(errs.KindOf(err))
		}
		status := http.StatusForbidden
		if errs.Is(err, errs.KindNotFound) {
			status = http.StatusNotFound
		}
		p.render(w, r, status, regTplError, regPage{TitleKey: "registration.title.error", ErrorKey: key})
	default:
		slog.Error("inscription : erreur interne", "erreur", err)
		p.render(w, r, http.StatusInternalServerError, regTplError, regPage{
			TitleKey: "registration.title.error", ErrorKey: "registration.error.internal"})
	}
}

// done rend l'écran de réussite : aucun secret, aucune paire.
func (p *registrationPages) done(w http.ResponseWriter, r *http.Request, g *groups.Group, s *verify.Success) {
	p.render(w, r, http.StatusOK, regTplDone, regPage{
		TitleKey: "registration.title.done", Slug: g.Slug, GroupName: g.Name,
		FirstName: s.FirstName, AlreadyVerified: s.AlreadyVerified})
}

// confirmPage sert le lien de confirmation en un clic (GET) : il affiche le
// code « déjà saisi » et un bouton, sans rien modifier. Un scanneur de
// courriel qui précharge le lien n'a aucun effet.
func (p *registrationPages) confirmPage(w http.ResponseWriter, r *http.Request) {
	g, ok := p.group(w, r)
	if !ok {
		return
	}
	if err := p.a.Invite.VerificationAllowed(r.Context(), g.ID); err != nil {
		p.fail(w, r, err)
		return
	}
	pv, err := p.a.Verify.PreviewLink(r.Context(), g.ID, r.PathValue("token"))
	if err != nil {
		p.fail(w, r, err)
		return
	}
	p.render(w, r, http.StatusOK, regTplConfirm, regPage{
		TitleKey: "registration.title.confirm", Slug: g.Slug, GroupName: g.Name,
		Action: r.URL.Path, Code: pv.Code})
}

// confirmSubmit consomme le jeton (POST seulement, jeton CSRF contrôlé par la
// chaîne) et vérifie l'inscription : le participant est créé par
// verify.ConfirmLink.
func (p *registrationPages) confirmSubmit(w http.ResponseWriter, r *http.Request) {
	g, ok := p.group(w, r)
	if !ok {
		return
	}
	if err := p.a.Invite.VerificationAllowed(r.Context(), g.ID); err != nil {
		p.fail(w, r, err)
		return
	}
	s, err := p.a.Verify.ConfirmLink(r.Context(), g.ID, r.PathValue("token"))
	if err != nil {
		p.fail(w, r, err)
		return
	}
	p.done(w, r, g, s)
}

// codeSubmit vérifie le code saisi pour l'inscription de référence « ref ».
func (p *registrationPages) codeSubmit(w http.ResponseWriter, r *http.Request) {
	g, ok := p.group(w, r)
	if !ok {
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxRegistrationFormBytes)
	if err := r.ParseForm(); err != nil {
		http.Error(w, "Requête invalide.", http.StatusBadRequest)
		return
	}
	ref, input := r.PostFormValue("ref"), r.PostFormValue("code")
	if err := p.a.Invite.VerificationAllowed(r.Context(), g.ID); err != nil {
		p.fail(w, r, err)
		return
	}
	s, err := p.a.Verify.VerifyCode(r.Context(), g.ID, ref, input)
	if code.IsRefused(err) {
		p.render(w, r, http.StatusUnprocessableEntity, regTplCode, regPage{
			TitleKey: "registration.title.code", Slug: g.Slug, GroupName: g.Name,
			Action: r.URL.Path, Ref: ref, ErrorKey: "registration.code.refused"})
		return
	}
	if err != nil {
		p.fail(w, r, err)
		return
	}
	p.done(w, r, g, s)
}

// registerRegistrationPages monte les routes de l'inscription sur Slugs, derrière
// la chaîne SecurityHeaders, CSRF, Sessions : le formulaire (GET et POST, motif
// avec et sans jeton), la saisie du code et le lien de confirmation en un clic
// (GET sans effet, POST qui vérifie).
func registerRegistrationPages(a *App) error {
	p, err := newRegistrationPages(a)
	if err != nil {
		return err
	}
	h := &invite.Handlers{
		Service: a.Invite, Render: p, Neutral: magic.NewNeutral(nil), Logger: slog.Default(), ClientIP: a.clientIP,
	}
	join := h.Join()
	a.Slugs.Handle(magic.PatternJoin, join)
	a.Slugs.Handle(magic.PatternJoinBare, join)
	a.Slugs.Handle("GET "+registrationConfirmPath, http.HandlerFunc(p.confirmPage))
	a.Slugs.Handle("POST "+registrationConfirmPath, websec.RequireUnsafeMethod(http.HandlerFunc(p.confirmSubmit)))
	a.Slugs.Handle("POST "+registrationCodePath, websec.RequireUnsafeMethod(http.HandlerFunc(p.codeSubmit)))
	return nil
}
