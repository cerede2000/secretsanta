// Package settingspages assemble la page des réglages d'administration (F10c.1,
// screen-admin-reglages) en sections repliables : Le groupe, Courrier,
// Relances, Données, Zone sensible, ainsi que la page de relance manuelle.
//
// Les réglages du groupe passent par groupsettings (F2e) : ils n'altèrent
// jamais une paire. Aucune paire n'est lue ni affichée ici. Les opérations de
// données (export, import, sauvegarde, duplication, suppression) restent celles
// du paquet adminops : cette page n'y renvoie que des formulaires.
package settingspages

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"html/template"
	"net/http"
	"strconv"
	"strings"
	"time"

	"secretsanta/internal/adminops"
	"secretsanta/internal/config"
	"secretsanta/internal/groups"
	"secretsanta/internal/groupsettings"
	"secretsanta/internal/i18n/negotiate"
	i18nweb "secretsanta/internal/i18n/web"
	"secretsanta/internal/mail/queue"
	"secretsanta/internal/participants"
	"secretsanta/internal/reminderrules"
	"secretsanta/internal/reminders"
	"secretsanta/internal/reveal"
	"secretsanta/internal/shared/adminnav"
	"secretsanta/internal/shared/errs"
	"secretsanta/internal/websec"
	"secretsanta/web"
)

// Groups est la part du service des groupes utilisée ici.
type Groups interface {
	BySlug(ctx context.Context, slug string) (*groups.Group, error)
}

// Settings est la part de groupsettings (F2e) utilisée ici.
type Settings interface {
	UpdateGroupSettings(ctx context.Context, in groupsettings.Input) (*groupsettings.Result, error)
	SetDefaultLanguage(ctx context.Context, groupID int64, lang string) error
	DefaultLanguage(ctx context.Context, groupID int64) (string, bool, error)
}

// Reveal est la part du service de révélation (F4d) utilisée ici.
type Reveal interface {
	Facts(ctx context.Context, a participants.Actor, groupID int64) (*reveal.Facts, error)
	SetRevealDate(ctx context.Context, a participants.Actor, groupID int64, at *time.Time) error
}

// Rhythm est la part de reminderrules (F7) utilisée ici.
type Rhythm interface {
	Settings(ctx context.Context, groupID int64) (reminderrules.Settings, error)
	SetSettings(ctx context.Context, groupID int64, st reminderrules.Settings) error
}

// Reminders est la part du moteur de relances (F7) utilisée ici.
type Reminders interface {
	Remind(ctx context.Context, groupID int64, kind reminders.Kind, ids []int64) (reminders.ManualResult, error)
}

// People liste les participants (service F3).
type People interface {
	List(ctx context.Context, a participants.Actor, groupID int64) ([]participants.Participant, error)
}

// MailTester envoie un courriel de test (*queue.Service).
type MailTester interface {
	SendTest(ctx context.Context, it queue.Item) (queue.Status, error)
}

// Ops est la part facultative d'adminops : dernière sauvegarde et plan de duplication.
type Ops interface {
	LastBackup(ctx context.Context, groupID int64) (*time.Time, error)
	PlanDuplicate(ctx context.Context, groupID int64) (*adminops.DuplicatePlan, error)
}

// Auditor consigne une action sensible (facultatif).
type Auditor interface {
	Record(ctx context.Context, a participants.Actor, groupID int64, action string, participantID int64) error
}

// Options rassemble les dépendances.
type Options struct {
	Groups     Groups
	Settings   Settings
	Reveal     Reveal
	Rhythm     Rhythm
	Reminders  Reminders
	People     People
	Mail       MailTester
	Ops        Ops
	Translator i18nweb.Translator
	Audit      Auditor
	// SMTP, Delivery et DKIM sont les valeurs de l'environnement, affichées en
	// lecture seule (aucun secret n'est affiché).
	SMTP     config.SMTP
	Delivery config.Mail
	DKIM     config.DKIM
	Location *time.Location
	NavFor   func(r *http.Request, active string) []adminnav.Item
}

// Pages construit les gestionnaires.
type Pages struct {
	o        Options
	settings *template.Template
	remind   *template.Template
}

// Routes.
const (
	RouteSettings     = "/{slug}/admin/reglages"
	RouteGroup        = "/{slug}/admin/reglages/groupe"
	RouteRhythm       = "/{slug}/admin/reglages/relances"
	RouteMailTest     = "/{slug}/admin/reglages/courrier/test"
	RouteRemind       = "/{slug}/admin/relances"
	DeliverabilityURL = "/{slug}/admin/courrier/delivrabilite"

	maxFormBytes = 16 << 10
	navKey       = "plus"
	dateLayout   = "2006-01-02T15:04"
)

// Actions consignées dans le journal.
const (
	ActionGroup  = "reglages_groupe"
	ActionRhythm = "reglages_relances"
	ActionRemind = "relance_manuelle"
)

// Sections.
const (
	secGroup  = "groupe"
	secMail   = "courrier"
	secRemind = "relances"
	secData   = "donnees"
	secDanger = "sensible"
)

// Clés de texte (module admin.settings).
const (
	keyTitle       = "admin.settings.title"
	keyRemindTitle = "admin.settings.remind.title"
	keyForbidden   = "admin.settings.error.forbidden"
	keyInternal    = "admin.settings.error.internal"
	keyInvalid     = "admin.settings.error.invalid"
	keyName        = "admin.settings.error.name"
	keyBudget      = "admin.settings.error.budget"
	keyEventDate   = "admin.settings.error.event_date"
	keyRevealDate  = "admin.settings.error.reveal_date"
	keyMaxRange    = "admin.settings.error.max_gifts_range"
	keyMaxConfirm  = "admin.settings.error.max_gifts_confirm"
	keyLang        = "admin.settings.error.lang"
	keyDelay       = "admin.settings.error.delay"
	keySpacing     = "admin.settings.error.spacing"
	keyCap         = "admin.settings.error.cap"
	keyMailTo      = "admin.settings.error.mail_to"
	keyMailOff     = "admin.settings.error.mail_off"
	keyMailFailed  = "admin.settings.error.mail_failed"
	keyNoPeople    = "admin.settings.error.no_people"
	keyKind        = "admin.settings.error.kind"
	keySavedGroup  = "admin.settings.notice.group"
	keySavedRhythm = "admin.settings.notice.rhythm"
)

// NewPages analyse les gabarits.
func NewPages(o Options) (*Pages, error) {
	if o.Groups == nil || o.Settings == nil || o.Reveal == nil || o.Rhythm == nil ||
		o.Reminders == nil || o.People == nil || o.Translator == nil {
		return nil, errors.New("settingspages: dépendance manquante")
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
	parse := func(file string) (*template.Template, error) {
		return template.New("").Funcs(funcs).ParseFS(web.Templates,
			"templates/layout/*.html", "templates/i18n/*.html", "templates/admin/settings/"+file)
	}
	st, err := parse("settings.html")
	if err != nil {
		return nil, fmt.Errorf("settingspages: gabarit : %w", err)
	}
	rt, err := parse("reminders.html")
	if err != nil {
		return nil, fmt.Errorf("settingspages: gabarit : %w", err)
	}
	return &Pages{o: o, settings: st, remind: rt}, nil
}

// groupForm est la saisie de la section « Le groupe ».
type groupForm struct {
	Name, Budget, EventDate, Reveal, Instructions string
	MaxGifts                                      int
	ConfirmMax                                    bool
	Notify                                        bool
	Lang                                          string
}

type langOption struct {
	Code, Name string
	Selected   bool
}

type rhythmForm struct {
	Enabled bool
	First   int
	Spacing int
	Cap     int
}

type smtpView struct {
	Enabled      bool
	Host         string
	Port         int
	Security     string
	From         string
	Domain       string
	EHLO         string
	DKIM         bool
	DKIMSelector string
	DKIMAlgo     string
	PerMinute    int
}

type dupView struct {
	Year int
	Name string
}

type pageData struct {
	Lang     string
	TitleKey string
	Nav      []adminnav.Item

	Slug      string
	GroupName string
	BackHref  string

	Open map[string]bool

	Form       groupForm
	Langs      []langOption
	RevealLock bool
	// ErrField et ErrKey : une erreur sous un champ. Les clés de champ sont
	// name, budget, event_date, reveal, max_gifts, lang, first, spacing, cap,
	// mail_to.
	ErrField string
	ErrKey   string
	// GlobalErr est une erreur qui ne vise aucun champ.
	GlobalErr string
	Notice    string

	SMTP     smtpView
	TestTo   string
	TestOK   string // clé de texte
	TestTime string
	TestErr  string // clé de texte
	TestRaw  string // dernière réponse du serveur

	Rhythm   rhythmForm
	Window   string
	Timezone string

	LastBackup string
	Dup        dupView

	// Adresses des opérations d'adminops et du diagnostic.
	DeliverabilityHref string
	ExportHref         string
	ImportAction       string
	BackupAction       string
	DuplicateAction    string
	DeleteAction       string
	RemindHref         string
	RhythmAction       string
	GroupAction        string
	MailTestAction     string

	// Relance manuelle.
	People    []remindPerson
	RemindAct string
	Kind      string
	Results   []remindResult
	Counts    []remindCount
	Done      bool
}

type remindPerson struct {
	ID      int64
	Name    string
	HasMail bool
	Checked bool
}

type remindResult struct {
	Name   string
	KeyOut string
}

type remindCount struct {
	KeyOut string
	N      int
}

func (p *Pages) render(w http.ResponseWriter, r *http.Request, tpl *template.Template, status int, d pageData) {
	clone, err := tpl.Clone()
	if err != nil {
		http.Error(w, "Internal error", http.StatusInternalServerError)
		return
	}
	clone.Funcs(websec.TemplateFuncs(r)).Funcs(i18nweb.Funcs(p.o.Translator, r))
	d.Lang = negotiate.Lang(r.Context(), p.o.Translator.DefaultLang())
	d.TitleKey = keyTitle
	if tpl == p.remind {
		d.TitleKey = keyRemindTitle
	}
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

func (p *Pages) record(ctx context.Context, a participants.Actor, groupID int64, action string) {
	if p.o.Audit != nil {
		_ = p.o.Audit.Record(ctx, a, groupID, action, 0)
	}
}

// statusKey traduit une erreur de service en clé de texte et statut HTTP.
func statusKey(err error) (string, int) {
	switch errs.KindOf(err) {
	case errs.KindForbidden, errs.KindUnauthorized:
		return keyForbidden, http.StatusForbidden
	case errs.KindValidation, errs.KindConflict:
		return keyInvalid, http.StatusUnprocessableEntity
	case errs.KindNotFound:
		return keyInvalid, http.StatusNotFound
	}
	return keyInternal, http.StatusInternalServerError
}

func budgetInput(cents *int64) string {
	if cents == nil {
		return ""
	}
	s := strconv.FormatInt(*cents/100, 10)
	if r := *cents % 100; r != 0 {
		s += "," + fmt.Sprintf("%02d", r)
	}
	return s
}

func (p *Pages) hasLang(code string) bool {
	for _, l := range p.o.Translator.Langs() {
		if l.Code == code {
			return true
		}
	}
	return false
}

func (p *Pages) langOptions(selected string) []langOption {
	var out []langOption
	for _, l := range p.o.Translator.Langs() {
		out = append(out, langOption{Code: l.Code, Name: l.Name, Selected: l.Code == selected})
	}
	return out
}

func (p *Pages) smtpView() smtpView {
	s, m, k := p.o.SMTP, p.o.Delivery, p.o.DKIM
	v := smtpView{Enabled: s.Enabled(), Host: s.Host, Port: s.Port, Security: m.TLSMode, From: s.From,
		EHLO: m.EHLOHostname, DKIM: k.Enabled(), DKIMSelector: k.Selector, DKIMAlgo: k.Algorithm, PerMinute: m.MaxPerMinute}
	if i := strings.LastIndex(s.From, "@"); i >= 0 {
		v.Domain = s.From[i+1:]
	}
	return v
}

// view lit l'état du groupe et prépare la page ; les formulaires reprennent
// les valeurs enregistrées.
func (p *Pages) view(ctx context.Context, g *groups.Group, a participants.Actor) (*pageData, error) {
	d := &pageData{Slug: g.Slug, GroupName: g.Name, BackHref: "/" + g.Slug + "/admin/plus", Open: map[string]bool{secGroup: true},
		DeliverabilityHref: strings.ReplaceAll(DeliverabilityURL, "{slug}", g.Slug),
		ExportHref:         "/" + g.Slug + "/admin/participants/export",
		ImportAction:       "/" + g.Slug + "/admin/participants/import",
		BackupAction:       "/" + g.Slug + "/admin/sauvegarde",
		DuplicateAction:    "/" + g.Slug + "/admin/dupliquer",
		DeleteAction:       "/" + g.Slug + "/admin/supprimer",
		RemindHref:         "/" + g.Slug + "/admin/relances",
		RhythmAction:       "/" + g.Slug + "/admin/reglages/relances",
		GroupAction:        "/" + g.Slug + "/admin/reglages/groupe",
		MailTestAction:     "/" + g.Slug + "/admin/reglages/courrier/test",
		SMTP:               p.smtpView(), Timezone: p.o.Location.String(),
		Window: fmt.Sprintf("%d–%d", reminderrules.WindowStartHour, reminderrules.WindowEndHour),
	}
	max := g.MaxGiftsPerGiver
	if max == 0 {
		max = groups.DefaultMaxGiftsPerGiver
	}
	d.Form = groupForm{Name: g.Name, Budget: budgetInput(g.BudgetCents), EventDate: g.EventDate,
		Instructions: g.GiftInstructions, MaxGifts: max}
	f, err := p.o.Reveal.Facts(ctx, a, g.ID)
	if err != nil {
		return nil, err
	}
	d.RevealLock = f.DateLocked
	if f.RevealAt != nil {
		d.Form.Reveal = f.RevealAt.In(p.o.Location).Format(dateLayout)
	}
	lang, _, err := p.o.Settings.DefaultLanguage(ctx, g.ID)
	if err != nil {
		return nil, err
	}
	d.Form.Lang = lang
	d.Langs = p.langOptions(lang)
	rs, err := p.o.Rhythm.Settings(ctx, g.ID)
	if err != nil {
		return nil, err
	}
	d.Rhythm = rhythmForm{Enabled: rs.Enabled, First: rs.FirstDelayDays, Spacing: rs.SpacingDays, Cap: rs.MaxPerType}
	if p.o.Ops != nil {
		if at, err := p.o.Ops.LastBackup(ctx, g.ID); err == nil && at != nil {
			d.LastBackup = at.In(p.o.Location).Format("2006-01-02")
		}
	}
	if p.o.Ops != nil {
		if plan, err := p.o.Ops.PlanDuplicate(ctx, g.ID); err == nil && plan != nil {
			d.Dup = dupView{Year: plan.Year, Name: plan.Name}
		}
	}
	return d, nil
}

func (p *Pages) fail(w http.ResponseWriter, r *http.Request, g *groups.Group, status int, key string) {
	p.render(w, r, p.settings, status, pageData{Slug: g.Slug, GroupName: g.Name, BackHref: "/" + g.Slug + "/admin/plus",
		Open: map[string]bool{}, GlobalErr: key, Langs: nil})
}

// show rend la page ; mod ajuste les données après la lecture de l'état.
func (p *Pages) show(w http.ResponseWriter, r *http.Request, g *groups.Group, a participants.Actor, status int, mod func(*pageData)) {
	d, err := p.view(r.Context(), g, a)
	if err != nil {
		key, st := statusKey(err)
		p.fail(w, r, g, st, key)
		return
	}
	if mod != nil {
		mod(d)
	}
	p.render(w, r, p.settings, status, *d)
}

// Page rend screen-admin-reglages (GET /{slug}/admin/reglages). Elle ne modifie rien.
func (p *Pages) Page() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		g, a, ok := p.admin(w, r)
		if !ok {
			return
		}
		p.show(w, r, g, a, http.StatusOK, func(d *pageData) {
			switch r.URL.Query().Get("ok") {
			case secGroup:
				d.Notice = keySavedGroup
			case secRemind:
				d.Notice = keySavedRhythm
				d.Open = map[string]bool{secRemind: true}
			}
		})
	})
}

// SaveGroup enregistre la section « Le groupe » (POST /{slug}/admin/reglages/groupe).
func (p *Pages) SaveGroup() http.Handler {
	return websec.RequireUnsafeMethod(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		g, a, ok := p.admin(w, r)
		if !ok {
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, maxFormBytes)
		if err := r.ParseForm(); err != nil {
			p.show(w, r, g, a, http.StatusBadRequest, func(d *pageData) { d.GlobalErr = keyInvalid })
			return
		}
		ctx := r.Context()
		f := groupForm{
			Name: r.PostForm.Get("name"), Budget: r.PostForm.Get("budget"), EventDate: r.PostForm.Get("event_date"),
			Reveal: r.PostForm.Get("reveal"), Instructions: r.PostForm.Get("instructions"),
			ConfirmMax: r.PostForm.Get("confirm_max_gifts") == "1", Notify: r.PostForm.Get("notify") == "1",
			Lang: r.PostForm.Get("lang"),
		}
		reject := func(field, key string, status int) {
			p.show(w, r, g, a, status, func(d *pageData) {
				lock := d.RevealLock
				d.Form, d.ErrField, d.ErrKey = f, field, key
				d.Langs = p.langOptions(f.Lang)
				d.RevealLock = lock
			})
		}
		mg, convErr := strconv.Atoi(strings.TrimSpace(r.PostForm.Get("max_gifts")))
		f.MaxGifts = mg
		if convErr != nil {
			f.MaxGifts = g.MaxGiftsPerGiver
		}
		// Contrôles par champ : chaque refus est un texte du catalogue sous le champ.
		if strings.TrimSpace(f.Name) == "" {
			reject("name", keyName, http.StatusUnprocessableEntity)
			return
		}
		if _, err := groups.ParseBudget(f.Budget); err != nil {
			reject("budget", keyBudget, http.StatusUnprocessableEntity)
			return
		}
		if ed := strings.TrimSpace(f.EventDate); ed != "" {
			if _, err := time.Parse(groups.DateLayout, ed); err != nil {
				reject("event_date", keyEventDate, http.StatusUnprocessableEntity)
				return
			}
		}
		if convErr != nil || mg < groups.MinMaxGiftsPerGiver || mg > groups.MaxMaxGiftsPerGiver {
			reject("max_gifts", keyMaxRange, http.StatusUnprocessableEntity)
			return
		}
		current := g.MaxGiftsPerGiver
		if current == 0 {
			current = groups.DefaultMaxGiftsPerGiver
		}
		var maxPtr *int
		if mg != current {
			maxPtr = &mg
			if mg > groups.DefaultMaxGiftsPerGiver && !f.ConfirmMax {
				reject("max_gifts", keyMaxConfirm, http.StatusUnprocessableEntity)
				return
			}
		}
		if f.Lang != "" && !p.hasLang(f.Lang) {
			reject("lang", keyLang, http.StatusUnprocessableEntity)
			return
		}
		facts, err := p.o.Reveal.Facts(ctx, a, g.ID)
		if err != nil {
			key, st := statusKey(err)
			p.fail(w, r, g, st, key)
			return
		}
		var revealAt *time.Time
		revealChanged := false
		if !facts.DateLocked {
			cur := ""
			if facts.RevealAt != nil {
				cur = facts.RevealAt.In(p.o.Location).Format(dateLayout)
			}
			if raw := strings.TrimSpace(f.Reveal); raw != cur {
				revealChanged = true
				if raw != "" {
					t, err := time.ParseInLocation(dateLayout, raw, p.o.Location)
					if err != nil {
						reject("reveal", keyRevealDate, http.StatusUnprocessableEntity)
						return
					}
					revealAt = &t
				}
			}
		}
		_, err = p.o.Settings.UpdateGroupSettings(ctx, groupsettings.Input{GroupID: g.ID, Name: f.Name, Budget: f.Budget,
			EventDate: strings.TrimSpace(f.EventDate), GiftInstructions: f.Instructions,
			MaxGiftsPerGiver: maxPtr, ConfirmMaxGifts: f.ConfirmMax, NotifyBudget: f.Notify})
		if err != nil {
			key, st := statusKey(err)
			reject("", key, st)
			return
		}
		if err := p.o.Settings.SetDefaultLanguage(ctx, g.ID, f.Lang); err != nil {
			reject("lang", keyLang, http.StatusUnprocessableEntity)
			return
		}
		if revealChanged {
			if err := p.o.Reveal.SetRevealDate(ctx, a, g.ID, revealAt); err != nil {
				reject("reveal", keyRevealDate, http.StatusUnprocessableEntity)
				return
			}
		}
		p.record(ctx, a, g.ID, ActionGroup)
		http.Redirect(w, r, "/"+g.Slug+"/admin/reglages?ok="+secGroup, http.StatusSeeOther)
	}))
}

// SaveRhythm enregistre le rythme des relances (POST /{slug}/admin/reglages/relances).
func (p *Pages) SaveRhythm() http.Handler {
	return websec.RequireUnsafeMethod(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		g, a, ok := p.admin(w, r)
		if !ok {
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, maxFormBytes)
		if err := r.ParseForm(); err != nil {
			p.show(w, r, g, a, http.StatusBadRequest, func(d *pageData) { d.GlobalErr = keyInvalid })
			return
		}
		num := func(name string) int {
			n, err := strconv.Atoi(strings.TrimSpace(r.PostForm.Get(name)))
			if err != nil {
				return -1
			}
			return n
		}
		st := reminderrules.Settings{Enabled: r.PostForm.Get("enabled") == "1",
			FirstDelayDays: num("first"), SpacingDays: num("spacing"), MaxPerType: num("cap")}
		reject := func(field, key string) {
			p.show(w, r, g, a, http.StatusUnprocessableEntity, func(d *pageData) {
				d.Open = map[string]bool{secRemind: true}
				d.Rhythm = rhythmForm{Enabled: st.Enabled, First: st.FirstDelayDays, Spacing: st.SpacingDays, Cap: st.MaxPerType}
				d.ErrField, d.ErrKey = field, key
			})
		}
		switch {
		case st.FirstDelayDays < reminderrules.MinDelayDays || st.FirstDelayDays > reminderrules.MaxDelayDays:
			reject("first", keyDelay)
			return
		case st.SpacingDays < reminderrules.MinDelayDays || st.SpacingDays > reminderrules.MaxDelayDays:
			reject("spacing", keySpacing)
			return
		case st.MaxPerType < reminderrules.MinMaxPerType || st.MaxPerType > reminderrules.MaxMaxPerType:
			reject("cap", keyCap)
			return
		}
		if err := p.o.Rhythm.SetSettings(r.Context(), g.ID, st); err != nil {
			key, code := statusKey(err)
			p.show(w, r, g, a, code, func(d *pageData) { d.Open = map[string]bool{secRemind: true}; d.GlobalErr = key })
			return
		}
		p.record(r.Context(), a, g.ID, ActionRhythm)
		http.Redirect(w, r, "/"+g.Slug+"/admin/reglages?ok="+secRemind, http.StatusSeeOther)
	}))
}

// MailTest envoie un courriel de test (POST /{slug}/admin/reglages/courrier/test).
func (p *Pages) MailTest() http.Handler {
	return websec.RequireUnsafeMethod(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		g, a, ok := p.admin(w, r)
		if !ok {
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, maxFormBytes)
		if err := r.ParseForm(); err != nil {
			p.show(w, r, g, a, http.StatusBadRequest, func(d *pageData) { d.GlobalErr = keyInvalid })
			return
		}
		to := strings.TrimSpace(r.PostForm.Get("test_to"))
		open := map[string]bool{secMail: true}
		show := func(status int, mod func(*pageData)) {
			p.show(w, r, g, a, status, func(d *pageData) { d.Open = open; d.TestTo = to; mod(d) })
		}
		if !p.o.SMTP.Enabled() || p.o.Mail == nil {
			show(http.StatusUnprocessableEntity, func(d *pageData) { d.TestErr = keyMailOff })
			return
		}
		if i := strings.Index(to, "@"); i < 1 || i == len(to)-1 || strings.ContainsAny(to, " \r\n<>,;") {
			show(http.StatusUnprocessableEntity, func(d *pageData) { d.ErrField, d.ErrKey = "mail_to", keyMailTo })
			return
		}
		lang := negotiate.Lang(r.Context(), p.o.Translator.DefaultLang())
		text := p.o.Translator.T(lang, "admin.settings.mail.test_body", nil)
		st, err := p.o.Mail.SendTest(r.Context(), queue.Item{GroupID: g.ID, To: to, Type: "test", Lang: lang,
			Subject: p.o.Translator.T(lang, "admin.settings.mail.test_subject", nil), Text: text, HTML: queue.HTMLFromText(text)})
		if err != nil {
			key, code := statusKey(err)
			show(code, func(d *pageData) { d.TestErr = key })
			return
		}
		if st.State == queue.StateSent {
			show(http.StatusOK, func(d *pageData) {
				d.TestOK = "admin.settings.mail.test_sent"
				d.TestTime = st.SentAt.In(p.o.Location).Format("15:04")
			})
			return
		}
		show(http.StatusOK, func(d *pageData) { d.TestErr = keyMailFailed; d.TestRaw = st.LastError })
	}))
}

var kinds = map[string]reminders.Kind{
	"lien": reminders.KindLink, "lettre": reminders.KindLetter, "lecture": reminders.KindRead,
}

func (p *Pages) remindPage(r *http.Request, g *groups.Group, a participants.Actor, kind string, checked map[int64]bool) (*pageData, error) {
	people, err := p.o.People.List(r.Context(), a, g.ID)
	if err != nil {
		return nil, err
	}
	d := &pageData{Slug: g.Slug, GroupName: g.Name, BackHref: "/" + g.Slug + "/admin/reglages", RemindAct: "/" + g.Slug + "/admin/relances", Kind: kind}
	if d.Kind == "" {
		d.Kind = "lien"
	}
	for _, pe := range people {
		d.People = append(d.People, remindPerson{ID: pe.ID, Name: pe.FirstName + " " + pe.Name, HasMail: pe.Email != "", Checked: checked[pe.ID]})
	}
	return d, nil
}

func (p *Pages) remindFail(w http.ResponseWriter, r *http.Request, g *groups.Group, status int, key string) {
	p.render(w, r, p.remind, status, pageData{Slug: g.Slug, GroupName: g.Name, BackHref: "/" + g.Slug + "/admin/reglages", GlobalErr: key})
}

// RemindPage rend la page de relance manuelle (GET /{slug}/admin/relances).
func (p *Pages) RemindPage() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		g, a, ok := p.admin(w, r)
		if !ok {
			return
		}
		d, err := p.remindPage(r, g, a, r.URL.Query().Get("type"), nil)
		if err != nil {
			key, st := statusKey(err)
			p.remindFail(w, r, g, st, key)
			return
		}
		p.render(w, r, p.remind, http.StatusOK, *d)
	})
}

var outcomeKey = map[reminders.Outcome]string{
	reminders.OutcomeQueued:         "admin.settings.remind.outcome.queued",
	reminders.OutcomeAlreadyToday:   "admin.settings.remind.outcome.already_today",
	reminders.OutcomeUnsubscribed:   "admin.settings.remind.outcome.unsubscribed",
	reminders.OutcomeAddressRefused: "admin.settings.remind.outcome.address_refused",
	reminders.OutcomeNoEmail:        "admin.settings.remind.outcome.no_email",
	reminders.OutcomeNotNeeded:      "admin.settings.remind.outcome.not_needed",
	reminders.OutcomeUnknown:        "admin.settings.remind.outcome.unknown",
}

// Remind relance à la main les personnes cochées (POST /{slug}/admin/relances).
func (p *Pages) Remind() http.Handler {
	return websec.RequireUnsafeMethod(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		g, a, ok := p.admin(w, r)
		if !ok {
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, maxFormBytes)
		if err := r.ParseForm(); err != nil {
			p.remindFail(w, r, g, http.StatusBadRequest, keyInvalid)
			return
		}
		kindName := r.PostForm.Get("type")
		checked := map[int64]bool{}
		var ids []int64
		for _, raw := range r.PostForm["person"] {
			if id, err := strconv.ParseInt(raw, 10, 64); err == nil && !checked[id] {
				checked[id] = true
				ids = append(ids, id)
			}
		}
		d, err := p.remindPage(r, g, a, kindName, checked)
		if err != nil {
			key, st := statusKey(err)
			p.remindFail(w, r, g, st, key)
			return
		}
		kind, known := kinds[kindName]
		switch {
		case !known:
			d.GlobalErr = keyKind
			p.render(w, r, p.remind, http.StatusUnprocessableEntity, *d)
			return
		case len(ids) == 0:
			d.GlobalErr = keyNoPeople
			p.render(w, r, p.remind, http.StatusUnprocessableEntity, *d)
			return
		}
		res, err := p.o.Reminders.Remind(r.Context(), g.ID, kind, ids)
		if err != nil {
			key, st := statusKey(err)
			d.GlobalErr = key
			p.render(w, r, p.remind, st, *d)
			return
		}
		p.record(r.Context(), a, g.ID, ActionRemind)
		d.Done = true
		order := []reminders.Outcome{reminders.OutcomeQueued, reminders.OutcomeAlreadyToday, reminders.OutcomeNotNeeded,
			reminders.OutcomeUnsubscribed, reminders.OutcomeAddressRefused, reminders.OutcomeNoEmail, reminders.OutcomeUnknown}
		for _, o := range order {
			if n := res.Count(o); n > 0 {
				d.Counts = append(d.Counts, remindCount{KeyOut: outcomeKey[o], N: n})
			}
		}
		for _, it := range res.Items {
			if it.Outcome != reminders.OutcomeQueued {
				d.Results = append(d.Results, remindResult{Name: it.Name, KeyOut: outcomeKey[it.Outcome]})
			}
		}
		p.render(w, r, p.remind, http.StatusOK, *d)
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
		{"GET " + RouteSettings, p.Page(), false},
		{"POST " + RouteGroup, p.SaveGroup(), true},
		{"POST " + RouteRhythm, p.SaveRhythm(), true},
		{"POST " + RouteMailTest, p.MailTest(), true},
		{"GET " + RouteRemind, p.RemindPage(), false},
		{"POST " + RouteRemind, p.Remind(), true},
	}
}

// Register branche les routes sur mux.
func (p *Pages) Register(mux *http.ServeMux) {
	for _, r := range p.Routes() {
		mux.Handle(r.Pattern, r.Handler)
	}
}
