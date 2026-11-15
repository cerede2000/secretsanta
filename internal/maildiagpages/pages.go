// Package maildiagpages sert la page d'administration « Délivrabilité »
// (screen-admin-delivrabilite) : rapport de diagnostic, enregistrements DNS à
// copier, courriel de test avec en-têtes bruts et liste de suppression.
//
// Les entrées du diagnostic viennent de la configuration, jamais de la page.
// Chaque route qui modifie l'état est en POST et enveloppée par
// websec.RequireUnsafeMethod.
package maildiagpages

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/mail"
	"net/url"
	"strconv"
	"strings"
	"time"

	"secretsanta/internal/groups"
	"secretsanta/internal/i18n/catalog"
	"secretsanta/internal/i18n/negotiate"
	"secretsanta/internal/mail/queue"
	"secretsanta/internal/maildiag"
	"secretsanta/internal/mailsuppress"
	"secretsanta/internal/shared/adminnav"
	"secretsanta/internal/shared/clock"
	"secretsanta/internal/shared/errs"
	"secretsanta/internal/websec"
)

// Gabarit exécuté et entrée de navigation active.
const (
	TemplateLayout = "layout/admin"
	NavKey         = "plus"
)

// Adresses des pages ({slug} est l'adresse du groupe).
const (
	PagePath      = "/{slug}/admin/courrier/delivrabilite"
	TestPath      = "/{slug}/admin/courrier/delivrabilite/test"
	RemovePath    = "/{slug}/admin/courrier/delivrabilite/suppression/retirer"
	LoginPath     = "/{slug}/admin/connexion"
	MorePath      = "/{slug}/admin/plus"
	removedQuery  = "retire"
	maxHeaderSize = 16 << 10
)

// GroupService est ce que les pages demandent au service des groupes.
type GroupService interface {
	BySlug(ctx context.Context, slug string) (*groups.Group, error)
}

// Diagnoser exécute le diagnostic (satisfait par *maildiag.Diagnostic).
type Diagnoser interface {
	Run(ctx context.Context) maildiag.Report
}

// Suppressions est la liste de suppression (satisfait par *mailsuppress.Store).
type Suppressions interface {
	List(ctx context.Context, groupID int64) ([]mailsuppress.Entry, error)
	Remove(ctx context.Context, groupID int64, address string) error
}

// TestMailer met un courriel de test en file (satisfait par *queue.Service).
type TestMailer interface {
	SendTest(ctx context.Context, it queue.Item) (queue.Status, error)
}

// Renderer écrit la page name avec le statut donné (fourni par l'orchestrateur).
type Renderer func(w http.ResponseWriter, r *http.Request, status int, name string, data any)

// Deps regroupe les dépendances.
type Deps struct {
	Groups       GroupService
	Diagnostic   Diagnoser
	Suppressions Suppressions
	Mailer       TestMailer
	// Builder et Signer (facultatif) fabriquent, comme la file, le message dont
	// la page affiche les en-têtes.
	Builder queue.Builder
	Signer  queue.Signer
	Catalog *catalog.Catalog
	Render  Renderer
	Nav     *adminnav.Registry
	Clock   clock.Clock
	// Location sert aux dates affichées (UTC si nil).
	Location    *time.Location
	DefaultLang string
	Logger      *slog.Logger
}

// Pages construit les gestionnaires.
type Pages struct{ d Deps }

// New valide les dépendances.
func New(d Deps) (*Pages, error) {
	if d.Groups == nil || d.Diagnostic == nil || d.Suppressions == nil || d.Mailer == nil ||
		d.Builder == nil || d.Catalog == nil || d.Render == nil || d.Nav == nil {
		return nil, errors.New("maildiagpages : Groups, Diagnostic, Suppressions, Mailer, Builder, Catalog, Render et Nav sont obligatoires")
	}
	if d.Clock == nil {
		d.Clock = clock.Real{}
	}
	if d.Location == nil {
		d.Location = time.UTC
	}
	if d.DefaultLang == "" {
		d.DefaultLang = "fr"
	}
	if d.Logger == nil {
		d.Logger = slog.Default()
	}
	return &Pages{d: d}, nil
}

// Register monte les routes sur mux (chaîne SecurityHeaders, CSRF, Sessions
// attendue en amont).
func (p *Pages) Register(mux *http.ServeMux) {
	mux.Handle("GET "+PagePath, p.Page())
	mux.Handle("POST "+TestPath, p.SendTest())
	mux.Handle("POST "+RemovePath, p.Remove())
}

func fill(path, slug string) string { return strings.ReplaceAll(path, "{slug}", slug) }

func (p *Pages) lang(r *http.Request) string { return negotiate.Lang(r.Context(), p.d.DefaultLang) }

func (p *Pages) t(lang, key string, kv ...any) string {
	return p.d.Catalog.T(lang, key, catalog.Pairs(kv...))
}

// admin renvoie le groupe si la session administrateur le concerne ; sinon il
// répond lui-même (404 groupe inconnu, 303 vers la connexion).
func (p *Pages) admin(w http.ResponseWriter, r *http.Request) (*groups.Group, bool) {
	g, err := p.d.Groups.BySlug(r.Context(), r.PathValue("slug"))
	if err != nil || g == nil {
		http.NotFound(w, r)
		return nil, false
	}
	if _, ok := websec.AdminSession(r.Context(), strconv.FormatInt(g.ID, 10)); !ok {
		http.Redirect(w, r, fill(LoginPath, g.Slug), http.StatusSeeOther)
		return nil, false
	}
	return g, true
}

func (p *Pages) nav(slug string) []adminnav.Item {
	items := p.d.Nav.Items(NavKey)
	out := make([]adminnav.Item, len(items))
	for i, it := range items {
		it.Destination = fill(it.Destination, slug)
		out[i] = it
	}
	return out
}

// Record est un enregistrement DNS à copier.
type Record struct {
	ID    string
	Name  string
	Type  string
	Value string
	// Text est ce que le bouton Copier place dans le presse-papiers.
	Text string
}

// Item est un point du rapport tel que le gabarit l'affiche.
type Item struct {
	ID     string
	Title  string
	Text   string
	Chip   string // classe : chip-ok, chip-error, maildiag-chip-off
	Label  string
	Record *Record
}

// Suppressed est une adresse de la liste de suppression.
type Suppressed struct {
	Address string
	Reason  string
	Date    string
}

// TestResult est le résultat du courriel de test.
type TestResult struct {
	To      string
	Text    string
	Failed  bool
	Headers string
}

// Page est la donnée du gabarit. Elle ne contient jamais de paire du tirage.
type Page struct {
	Lang     string
	TitleKey string
	Nav      []adminnav.Item

	Slug      string
	BackURL   string
	PageURL   string
	TestURL   string
	RemoveURL string

	Domain    string
	CheckedAt string
	OK, Fix   int
	Off       int
	Items     []Item
	Unverif   []string
	GuideURL  string

	TestTo    string
	TestError string
	Test      *TestResult

	Suppressed []Suppressed
	Removed    bool
}

var statusClass = map[maildiag.Status]struct{ chip, key string }{
	maildiag.StatusOK:    {"chip-ok", "maildiag.status.ok"},
	maildiag.StatusWarn:  {"chip-error", "maildiag.status.fix"},
	maildiag.StatusError: {"chip-error", "maildiag.status.fix"},
	maildiag.StatusOff:   {"maildiag-chip-off", "maildiag.status.off"},
}

func (p *Pages) build(r *http.Request, g *groups.Group) (Page, error) {
	lang := p.lang(r)
	pg := Page{
		Lang: lang, TitleKey: "maildiag.title", Nav: p.nav(g.Slug), Slug: g.Slug,
		BackURL: fill(MorePath, g.Slug), PageURL: fill(PagePath, g.Slug),
		TestURL: fill(TestPath, g.Slug), RemoveURL: fill(RemovePath, g.Slug),
		GuideURL: maildiag.GuideAnchor,
	}
	rep := p.d.Diagnostic.Run(r.Context())
	pg.Domain = rep.Domain
	pg.CheckedAt = p.d.Clock.Now().In(p.d.Location).Format("2006-01-02 15:04")
	for _, c := range rep.Checks {
		cl := statusClass[c.Status]
		it := Item{ID: "maildiag-" + strings.ReplaceAll(c.ID, "_", "-"), Title: p.t(lang, "maildiag.check."+c.ID),
			Text: p.t(lang, "maildiag.code."+c.Code, paramPairs(c.Params)...), Chip: cl.chip, Label: p.t(lang, cl.key)}
		switch c.Status {
		case maildiag.StatusOK:
			pg.OK++
		case maildiag.StatusOff:
			pg.Off++
		default:
			pg.Fix++
		}
		if c.Record != nil {
			it.Record = &Record{ID: it.ID + "-record", Name: c.Record.Name, Type: c.Record.Type, Value: c.Record.Value,
				Text: c.Record.Name + " " + c.Record.Type + " " + c.Record.Value}
		}
		pg.Items = append(pg.Items, it)
	}
	for _, nv := range rep.NotVerifiable {
		pg.Unverif = append(pg.Unverif, "maildiag.unverifiable."+nv.ID)
	}
	entries, err := p.d.Suppressions.List(r.Context(), g.ID)
	if err != nil {
		return pg, err
	}
	for _, e := range entries {
		pg.Suppressed = append(pg.Suppressed, Suppressed{Address: e.Address, Reason: e.Reason,
			Date: e.At.In(p.d.Location).Format("2006-01-02")})
	}
	return pg, nil
}

func paramPairs(m map[string]string) []any {
	out := make([]any, 0, 2*len(m))
	for k, v := range m {
		out = append(out, k, v)
	}
	return out
}

func (p *Pages) fail(w http.ResponseWriter, r *http.Request, err error) {
	p.d.Logger.Error("page de délivrabilité", "err", err)
	http.Error(w, "Erreur interne.", http.StatusInternalServerError)
}

// Page est la page de diagnostic (lecture seule).
func (p *Pages) Page() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		g, ok := p.admin(w, r)
		if !ok {
			return
		}
		pg, err := p.build(r, g)
		if err != nil {
			p.fail(w, r, err)
			return
		}
		pg.Removed = r.URL.Query().Get(removedQuery) == "1"
		p.d.Render(w, r, http.StatusOK, TemplateLayout, pg)
	})
}

// SendTest met un courriel de test en file et affiche les en-têtes bruts du
// message.
func (p *Pages) SendTest() http.Handler {
	return websec.RequireUnsafeMethod(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		g, ok := p.admin(w, r)
		if !ok {
			return
		}
		lang := p.lang(r)
		to := strings.TrimSpace(r.FormValue("to"))
		status := http.StatusOK
		var res *TestResult
		var fieldErr string
		if a, err := mail.ParseAddress(to); err != nil || a.Address != to || strings.ContainsAny(to, "\r\n") {
			fieldErr = p.t(lang, "maildiag.test.invalid")
			status = http.StatusUnprocessableEntity
		} else {
			res = p.sendTest(r.Context(), g, lang, to)
		}
		pg, err := p.build(r, g)
		if err != nil {
			p.fail(w, r, err)
			return
		}
		pg.TestTo, pg.TestError, pg.Test = to, fieldErr, res
		p.d.Render(w, r, status, TemplateLayout, pg)
	}))
}

func (p *Pages) sendTest(ctx context.Context, g *groups.Group, lang, to string) *TestResult {
	text := p.t(lang, "maildiag.mail.text", "group", g.Name)
	it := queue.Item{GroupID: g.ID, To: to, Type: "test", Essential: true,
		Subject: p.t(lang, "maildiag.mail.subject"), Text: text, HTML: queue.HTMLFromText(text)}
	res := &TestResult{To: to}
	st, err := p.d.Mailer.SendTest(ctx, it)
	switch {
	case err != nil:
		res.Failed = true
		res.Text = p.t(lang, "maildiag.test.error", "error", queue.Clean(err))
		return res
	case st.State == queue.StateSent:
		res.Text = p.t(lang, "maildiag.test.sent", "to", to, "time", st.SentAt.In(p.d.Location).Format("15:04"))
	case st.State == queue.StateFailed:
		res.Failed = true
		res.Text = p.t(lang, "maildiag.test.failed", "error", st.LastError)
	default:
		res.Failed = st.LastError != ""
		res.Text = p.t(lang, "maildiag.test.queued", "error", st.LastError)
	}
	res.Headers = p.headers(it)
	return res
}

// headers fabrique le message comme la file et renvoie ses en-têtes bruts.
func (p *Pages) headers(it queue.Item) string {
	built, err := p.d.Builder.Build(messageOf(it))
	if err != nil {
		return ""
	}
	raw := built.Raw
	if p.d.Signer != nil {
		if s, err := p.d.Signer.Sign(raw); err == nil {
			raw = s
		}
	}
	if i := bytes.Index(raw, []byte("\r\n\r\n")); i >= 0 {
		raw = raw[:i]
	}
	if len(raw) > maxHeaderSize {
		raw = raw[:maxHeaderSize]
	}
	return strings.ReplaceAll(string(raw), "\r\n", "\n")
}

// Remove retire une adresse de la liste de suppression du groupe de la session.
func (p *Pages) Remove() http.Handler {
	return websec.RequireUnsafeMethod(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		g, ok := p.admin(w, r)
		if !ok {
			return
		}
		addr := strings.TrimSpace(r.FormValue("address"))
		if addr == "" {
			http.Error(w, errs.Validation("L'adresse est obligatoire.").Error(), http.StatusUnprocessableEntity)
			return
		}
		if err := p.d.Suppressions.Remove(r.Context(), g.ID, addr); err != nil {
			p.fail(w, r, err)
			return
		}
		q := url.Values{removedQuery: {"1"}}
		http.Redirect(w, r, fill(PagePath, g.Slug)+"?"+q.Encode()+"#maildiag-suppression", http.StatusSeeOther)
	}))
}
