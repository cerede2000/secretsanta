package app

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"secretsanta/internal/draw"
	"secretsanta/internal/grouppages"
	"secretsanta/internal/groups"
	"secretsanta/internal/groupsettings"
	"secretsanta/internal/i18n/catalog"
	"secretsanta/internal/i18n/negotiate"
	"secretsanta/internal/mail/queue"
	"secretsanta/internal/mail/templates"
	"secretsanta/internal/participants"
	"secretsanta/internal/reveal"
	"secretsanta/internal/shared/clock"
	"secretsanta/internal/shared/errs"
	"secretsanta/internal/shared/scheduler"
	"secretsanta/internal/websec"
)

// Chemins de la révélation côté participant (sous l'adresse du groupe).
const (
	revealPagePath    = "/{slug}/decouverte"
	revealTargetsPath = "/{slug}/decouverte/cibles"
	revealConfirmPath = "/{slug}/decouverte/confirmation"
	revealTemplate    = "layout/base"
	// maxConfirmBytes borne le corps JSON de la confirmation.
	maxConfirmBytes = 1 << 10
	// drawRetryEvery et drawRetryMax règlent la reprise de la révélation quand
	// elle échoue juste après un tirage.
	drawRetryEvery = time.Minute
	drawRetryMax   = 5
)

// Drawing enchaîne le tirage et la révélation. Tout tirage, nouveau tirage ou
// ajustement du tirage passe par lui : une fois le changement enregistré, la
// révélation est mise à jour (ouverture si elle est due, sinon tâche planifiée
// à la date) et les avis « tirage prêt » partent. Appeler directement
// a.Draw.Draw, Redraw ou AdjustTx contournerait la révélation.
type Drawing struct {
	svc    *draw.Service
	reveal *reveal.Service
	db     *sql.DB
	clock  clock.Clock
	sched  *scheduler.Scheduler
}

// Draw lance le premier tirage du groupe.
func (d *Drawing) Draw(ctx context.Context, a participants.Actor, groupID int64, req draw.Request) (*draw.Outcome, error) {
	out, err := d.svc.Draw(ctx, a, groupID, req)
	if err != nil {
		return nil, err
	}
	d.changed(ctx, groupID)
	return out, nil
}

// Redraw refait le tirage complet.
func (d *Drawing) Redraw(ctx context.Context, a participants.Actor, groupID int64, req draw.Request) (*draw.Outcome, error) {
	out, err := d.svc.Redraw(ctx, a, groupID, req)
	if err != nil {
		return nil, err
	}
	d.changed(ctx, groupID)
	return out, nil
}

// Adjust remplace les paires du tirage actuel (administrateur du groupe) dans
// une transaction, puis met la révélation à jour. Seule la cohérence
// structurelle est vérifiée (voir draw.Service.AdjustTx) ; les règles sont
// l'affaire de l'appelant.
func (d *Drawing) Adjust(ctx context.Context, a participants.Actor, groupID int64, pairs []draw.Pair) error {
	switch {
	case a.Kind == participants.ActorAnonymous:
		return errs.New(errs.KindUnauthorized, "Authentification requise.")
	case a.Kind != participants.ActorAdmin || a.GroupID != groupID:
		return errs.Forbidden("Tu n'as pas le droit d'effectuer cette action.")
	}
	tx, err := d.db.BeginTx(ctx, nil)
	if err != nil {
		return errs.Internal(err)
	}
	defer func() { _ = tx.Rollback() }()
	if err := d.svc.AdjustTx(ctx, tx, groupID, pairs); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return errs.Internal(err)
	}
	d.changed(ctx, groupID)
	return nil
}

// changed prévient la révélation. Le changement est déjà enregistré : une
// erreur ne le défait pas ; elle est journalisée et la mise à jour est
// retentée par l'ordonnanceur (puis, au pire, par Restore au redémarrage).
func (d *Drawing) changed(ctx context.Context, groupID int64) {
	err := d.reveal.OnDrawChanged(ctx, groupID)
	if err == nil {
		return
	}
	slog.Error("révélation non mise à jour après un changement du tirage", "groupe", groupID, "erreur", err)
	d.retry(groupID, 1)
}

func (d *Drawing) retry(groupID int64, attempt int) {
	if attempt > drawRetryMax {
		return
	}
	d.sched.At(d.clock.Now().Add(drawRetryEvery), func(ctx context.Context) error {
		if err := d.reveal.OnDrawChanged(ctx, groupID); err != nil {
			slog.Error("révélation toujours pas mise à jour", "groupe", groupID, "tentative", attempt, "erreur", err)
			d.retry(groupID, attempt+1)
		}
		return nil
	})
}

// registerReveal construit le service de révélation : tâche d'ouverture sur
// l'ordonnanceur, avis « tirage prêt » mis en file par readyMailer (seulement
// si la file tourne, c'est-à-dire avec un transport ; sinon les avis restent en
// attente et partent au premier démarrage qui en a un), reprise des
// ouvertures au démarrage. Elle suit registerDraw et registerMailQueue.
func registerReveal(a *App) error {
	var mailer reveal.ReadyMailer
	if a.mail.q != nil {
		mailer = readyMailer{
			q: a.mail.q, cat: a.Cat, groups: a.Groups, settings: a.GroupSettings, parts: a.Participants,
			base: a.publicBase(),
		}
	}
	a.Reveal = reveal.NewService(a.DB, a.Clock, a.sched, a.Draw, mailer, reveal.Options{})
	a.Drawing = &Drawing{svc: a.Draw, reveal: a.Reveal, db: a.DB, clock: a.Clock, sched: a.sched}
	a.OnStart(func(ctx context.Context) {
		if err := a.Reveal.Restore(ctx); err != nil && !errors.Is(err, context.Canceled) {
			slog.Error("reprise des révélations en échec", "erreur", err)
		}
	})
	return nil
}

// readyMailer implémente reveal.ReadyMailer : il rend le courriel « tirage
// prêt » (variante choisie par templates.ReadyKind) et le met en file. Il ne
// connaît ni tireur ni cible : le courriel ne porte que le nombre de cibles.
type readyMailer struct {
	q        *queue.Service
	cat      *catalog.Catalog
	groups   *groups.Service
	settings *groupsettings.Service
	parts    *participants.Service
	base     string
}

// EnqueueReady met l'avis en file. Un participant supprimé (ou sans adresse)
// n'a personne à prévenir : l'avis est considéré comme traité (nil).
func (m readyMailer) EnqueueReady(ctx context.Context, n reveal.ReadyNotice) error {
	p, err := m.parts.Get(ctx, participants.AdminActor(n.GroupID), n.GroupID, n.ParticipantID)
	if err != nil {
		if errs.Is(err, errs.KindNotFound) {
			return nil
		}
		return err
	}
	if strings.TrimSpace(p.Email) == "" {
		return nil
	}
	g, err := m.groups.ByID(ctx, n.GroupID)
	if err != nil {
		return err
	}
	first := strings.TrimSpace(p.FirstName)
	if first == "" {
		first = localPart(p.Email)
	}
	kind := templates.ReadyKind(n.ReceptionOnly, n.Targets)
	d := templates.Data{
		To: p.Email, FirstName: first, ParticipantLang: p.Locale,
		GroupName: g.Name, BudgetCents: g.BudgetCents,
		// Le lien personnel d'origine n'est conservé que sous forme d'empreinte
		// et en émettre un nouveau couperait celui de l'invitation : le lien
		// mène à la page de découverte, qui renvoie à la connexion si la
		// personne n'est pas connectée.
		Link: m.base + "/" + g.Slug + "/decouverte",
	}
	if l, ok := m.settings.GroupLanguage(ctx, g.Slug); ok {
		d.GroupLang = l
	}
	msg, err := templates.Render(m.cat, kind, d)
	if err != nil {
		return err
	}
	_, err = m.q.EnqueueMail(ctx, queue.Item{
		GroupID: g.ID, ParticipantID: p.ID, To: p.Email, ToName: first, Type: string(kind),
		Essential: msg.Essential, Subject: msg.Subject, Text: msg.Text, HTML: msg.HTML,
	})
	return err
}

var _ reveal.ReadyMailer = readyMailer{}

// revealPages porte les routes de découverte de la cible : la page, la liste
// JSON des cibles de la personne connectée et la confirmation de découverte.
// Les trois exigent la session participant du groupe ; la liste ne renvoie
// rien tant que la révélation n'est pas ouverte (draw.MyTargets).
type revealPages struct {
	a    *App
	page *templateSet
	oops *templateSet
}

// registerRevealPages monte les routes de découverte sur Slugs. Elles suivent
// registerAccessPages (même famille de routes /{slug}/...).
func registerRevealPages(a *App) error {
	page, err := newTemplateSet(a.Cat, "reveal", "reveal/envelope", "reveal/gift", "reveal/scratch")
	if err != nil {
		return err
	}
	oops, err := newTemplateSet(a.Cat, "groups")
	if err != nil {
		return err
	}
	p := &revealPages{a: a, page: page, oops: oops}
	a.Slugs.Handle("GET "+revealPagePath, http.HandlerFunc(p.showPage))
	a.Slugs.Handle("GET "+revealTargetsPath, http.HandlerFunc(p.targets))
	a.Slugs.Handle("POST "+revealConfirmPath, websec.RequireUnsafeMethod(http.HandlerFunc(p.confirm)))
	return nil
}

func (p *revealPages) langOf(r *http.Request) string {
	return negotiate.Lang(r.Context(), p.a.Cfg.DefaultLang)
}

// who résout le groupe de l'adresse et la personne connectée. Quand ok est
// faux, la réponse est déjà écrite : page neutre 404 (groupe inconnu), renvoi
// vers la connexion (page sans session) ou erreur JSON (appels du script).
func (p *revealPages) who(w http.ResponseWriter, r *http.Request, asPage bool) (g *groups.Group, pid int64, ok bool) {
	g, err := p.a.Groups.BySlug(r.Context(), r.PathValue("slug"))
	if err != nil {
		status, key := http.StatusNotFound, "core.errors.not_found"
		if !errs.Is(err, errs.KindNotFound) {
			status, key = http.StatusInternalServerError, "core.errors.internal"
		}
		if asPage {
			p.oops.render(w, r, status, grouppages.TemplateError,
				grouppages.Page{Lang: p.langOf(r), TitleKey: "groups.error.title", ErrorKey: key})
		} else {
			p.jsonError(w, r, status, key)
		}
		return nil, 0, false
	}
	sess, found := websec.ParticipantSession(r.Context(), strconv.FormatInt(g.ID, 10))
	if found {
		pid, err = strconv.ParseInt(sess.Subject, 10, 64)
		found = err == nil && pid > 0
	}
	if !found {
		if asPage {
			w.Header().Set("Cache-Control", "no-store")
			http.Redirect(w, r, "/"+g.Slug+"/connexion", http.StatusSeeOther)
		} else {
			p.jsonError(w, r, http.StatusUnauthorized, errs.KindKey(errs.KindUnauthorized))
		}
		return nil, 0, false
	}
	return g, pid, true
}

// showPage sert la page de découverte (aucun effet : GET). Elle ne contient
// aucune cible ; le script les demande à la liste JSON.
func (p *revealPages) showPage(w http.ResponseWriter, r *http.Request) {
	g, _, ok := p.who(w, r, true)
	if !ok {
		return
	}
	base := "/" + g.Slug + "/decouverte"
	p.page.render(w, r, http.StatusOK, revealTemplate, map[string]any{
		"Lang":       p.langOf(r),
		"TitleKey":   "reveal.core.title.page",
		"TargetsURL": base + "/cibles",
		"ConfirmURL": base + "/confirmation",
		"LetterURL":  "",
	})
}

// targetView est une cible telle que le script la lit.
type targetView struct {
	ID       int64  `json:"id"`
	Name     string `json:"name"`
	Hint     string `json:"hint,omitempty"`
	Budget   string `json:"budget,omitempty"`
	Deadline string `json:"deadline,omitempty"`
}

// targets renvoie les cibles de la personne connectée : aucune tant que la
// révélation n'est pas ouverte. L'identifiant renvoyé est celui de la paire,
// que la confirmation rend ; il ne désigne que la propre cible de la personne.
func (p *revealPages) targets(w http.ResponseWriter, r *http.Request) {
	g, pid, ok := p.who(w, r, false)
	if !ok {
		return
	}
	ctx := r.Context()
	mine, err := p.a.Draw.MyTargets(ctx, participants.ParticipantActor(g.ID, pid), g.ID)
	if err != nil {
		p.fail(w, r, err)
		return
	}
	lang := p.langOf(r)
	out := []targetView{}
	for _, t := range mine.Targets {
		// Lecture avec l'acteur administrateur du groupe, bornée aux cibles de
		// la personne connectée (le service refuse à un participant de lire un
		// autre participant).
		rec, err := p.a.Participants.Get(ctx, participants.AdminActor(g.ID), g.ID, t.ReceiverID)
		if err != nil {
			if errs.Is(err, errs.KindNotFound) {
				continue
			}
			p.fail(w, r, err)
			return
		}
		v := targetView{ID: t.PairID, Name: strings.TrimSpace(rec.FirstName + " " + rec.Name), Hint: rec.GiftHint}
		if g.BudgetCents != nil {
			v.Budget = templates.FormatBudget(lang, *g.BudgetCents)
		}
		v.Deadline = eventDateLabel(lang, g.EventDate)
		out = append(out, v)
	}
	p.writeJSON(w, http.StatusOK, map[string]any{"targets": out})
}

// confirm enregistre la découverte d'une cible de la personne connectée (POST,
// jeton CSRF dans l'en-tête X-CSRF-Token, contrôlé par la chaîne).
func (p *revealPages) confirm(w http.ResponseWriter, r *http.Request) {
	g, pid, ok := p.who(w, r, false)
	if !ok {
		return
	}
	var body struct {
		TargetID int64 `json:"target_id"`
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxConfirmBytes)
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.TargetID <= 0 {
		p.jsonError(w, r, http.StatusBadRequest, errs.KindKey(errs.KindValidation))
		return
	}
	first, err := p.a.Draw.MarkDiscovered(r.Context(), participants.ParticipantActor(g.ID, pid), g.ID, body.TargetID)
	if err != nil {
		p.fail(w, r, err)
		return
	}
	p.writeJSON(w, http.StatusOK, map[string]any{"first": first})
}

// fail traduit une erreur de service en réponse JSON : le texte vient du
// catalogue (code de l'erreur s'il existe, sinon la clé de sa nature), jamais
// du message interne.
func (p *revealPages) fail(w http.ResponseWriter, r *http.Request, err error) {
	status := http.StatusInternalServerError
	switch errs.KindOf(err) {
	case errs.KindValidation:
		status = http.StatusBadRequest
	case errs.KindNotFound:
		status = http.StatusNotFound
	case errs.KindConflict:
		status = http.StatusConflict
	case errs.KindForbidden:
		status = http.StatusForbidden
	case errs.KindUnauthorized:
		status = http.StatusUnauthorized
	default:
		slog.Error("découverte : erreur interne", "erreur", err)
	}
	key := errs.KindKey(errs.KindOf(err))
	if code, _ := errs.CodeOf(err); code != "" {
		if _, known := p.a.Cat.Messages(p.langOf(r))[code]; known {
			key = code
		}
	}
	p.jsonError(w, r, status, key)
}

func (p *revealPages) jsonError(w http.ResponseWriter, r *http.Request, status int, key string) {
	p.writeJSON(w, status, map[string]any{"code": key, "message": p.a.Cat.T(p.langOf(r), key, nil)})
}

func (p *revealPages) writeJSON(w http.ResponseWriter, status int, v any) {
	b, err := json.Marshal(v)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	h := w.Header()
	h.Set("Content-Type", "application/json; charset=utf-8")
	h.Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_, _ = w.Write(b)
}

// eventDateLabel écrit la date du jour J (AAAA-MM-JJ) selon la langue ; vide
// si le groupe n'en a pas ou si elle est illisible.
func eventDateLabel(lang, iso string) string {
	if strings.TrimSpace(iso) == "" {
		return ""
	}
	t, err := time.Parse(groups.DateLayout, iso)
	if err != nil {
		return ""
	}
	if strings.HasPrefix(strings.ToLower(lang), "fr") {
		return t.Format("02/01/2006")
	}
	return t.Format("2006-01-02")
}
