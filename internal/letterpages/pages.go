// Package letterpages porte les pages de la lettre au Père Noël (F5c.1) : la
// page d'écriture du tiré (screen-lettre, avec l'arrivée bienvenue=1) et la
// page de lecture seule du tireur (screen-lettre-recue). Les gestionnaires
// sont construits ici et branchés par l'orchestrateur (voir Routes).
//
// Règles : la page d'écriture ne porte jamais d'identifiant de lettre, elle
// agit toujours sur la lettre de la personne connectée ; la page de lecture
// passe par letterread (révélation, découverte, cible) ; le budget n'est
// affiché que s'il est défini ; aucun texte brut, style ou script en ligne.
package letterpages

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"html/template"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"secretsanta/internal/draw"
	"secretsanta/internal/groups"
	"secretsanta/internal/i18n/negotiate"
	i18nweb "secretsanta/internal/i18n/web"
	"secretsanta/internal/letter"
	"secretsanta/internal/letterread"
	"secretsanta/internal/participants"
	"secretsanta/internal/shared/clock"
	"secretsanta/internal/shared/errs"
	"secretsanta/internal/websec"
	"secretsanta/web"
)

// Groups est la part du service des groupes (F2) utilisée ici.
type Groups interface {
	BySlug(ctx context.Context, slug string) (*groups.Group, error)
}

// Letters est la part du service de la lettre (F5) utilisée ici.
type Letters interface {
	Get(ctx context.Context, groupID, participantID int64) (*letter.Letter, error)
	SetIntro(ctx context.Context, groupID, participantID int64, intro string) (*letter.Letter, error)
	AddIdea(ctx context.Context, groupID, participantID int64, in letter.IdeaInput) (int64, error)
	DeleteIdea(ctx context.Context, groupID, participantID, ideaID int64) error
	ImageFile(ctx context.Context, groupID, ideaID int64) ([]byte, string, error)
}

// Reader est la part du service de lecture (F5b) utilisée ici
// (satisfait par *letterread.Service).
type Reader interface {
	Read(ctx context.Context, a participants.Actor, groupID, pairID int64) (*letter.Letter, error)
}

// Targets donne les cibles de la personne connectée (satisfait par
// *draw.Service).
type Targets interface {
	MyTargets(ctx context.Context, a participants.Actor, groupID int64) (*draw.MyTargets, error)
}

// Names donne le prénom d'un participant du groupe ; l'orchestrateur
// l'adapte sur participants.Service. Il n'est appelé que pour la personne
// connectée ou pour une cible déjà découverte.
type Names interface {
	FirstName(ctx context.Context, groupID, participantID int64) (string, error)
}

// Options rassemble les dépendances.
type Options struct {
	Groups     Groups
	Letters    Letters
	Reader     Reader
	Targets    Targets
	Names      Names
	Translator i18nweb.Translator
	Clock      clock.Clock
	// Location sert à l'heure « Enregistré à … » (défaut : UTC).
	Location *time.Location
	// MailboxURL, HomeURL : pages de retour (défauts : /<slug>/boite-aux-lettres
	// et /<slug>/participant).
	MailboxURL func(slug string) string
	HomeURL    func(slug string) string
}

// Pages construit les gestionnaires.
type Pages struct {
	o   Options
	tpl map[string]*template.Template
}

const (
	tplWrite    = "write"
	tplReceived = "received"

	// Routes (patterns complets pour http.ServeMux).
	RouteWrite    = "/{slug}/lettre"
	RouteIdeas    = "/{slug}/lettre/idees"
	RouteDelete   = "/{slug}/lettre/idees/{idee}/supprimer"
	RouteImage    = "/{slug}/lettre/image/{idee}"
	RouteReceived = "/{slug}/lettre-recue/{cible}"

	maxIntroForm = 16 << 10
	maxIdeaForm  = letter.ImageMaxBytes + (256 << 10)
)

// New analyse les gabarits (mise en page et sélecteur de langue compris).
func New(o Options) (*Pages, error) {
	if o.Groups == nil || o.Letters == nil || o.Reader == nil || o.Targets == nil || o.Names == nil || o.Translator == nil || o.Clock == nil {
		return nil, errors.New("letterpages: dépendance manquante")
	}
	if o.Location == nil {
		o.Location = time.UTC
	}
	if o.MailboxURL == nil {
		o.MailboxURL = func(s string) string { return "/" + s + "/boite-aux-lettres" }
	}
	if o.HomeURL == nil {
		o.HomeURL = func(s string) string { return "/" + s + "/participant" }
	}
	p := &Pages{o: o, tpl: map[string]*template.Template{}}
	funcs := template.FuncMap{}
	for k, v := range websec.TemplateFuncsPlaceholder() {
		funcs[k] = v
	}
	for k, v := range i18nweb.FuncsPlaceholder() {
		funcs[k] = v
	}
	for _, name := range []string{tplWrite, tplReceived} {
		t, err := template.New("").Funcs(funcs).ParseFS(web.Templates,
			"templates/layout/*.html", "templates/i18n/*.html", "templates/letter/"+name+".html")
		if err != nil {
			return nil, fmt.Errorf("letterpages: gabarit %s : %w", name, err)
		}
		p.tpl[name] = t
	}
	return p, nil
}

// Route décrit une route à enregistrer.
type Route struct {
	Pattern string
	Handler http.Handler
	// Modifies est vrai pour les routes qui changent l'état (POST, déjà
	// enveloppées par websec.RequireUnsafeMethod).
	Modifies bool
}

// Routes liste les routes, à enregistrer derrière SecurityHeaders, CSRF,
// Sessions.
func (p *Pages) Routes() []Route {
	return []Route{
		{"GET " + RouteWrite, p.WritePage(), false},
		{"POST " + RouteWrite, p.SaveIntro(), true},
		{"POST " + RouteIdeas, p.AddIdea(), true},
		{"POST " + RouteDelete, p.DeleteIdea(), true},
		{"GET " + RouteImage, p.OwnImage(), false},
		{"GET " + RouteReceived, p.ReceivedPage(), false},
	}
}

// Register enregistre toutes les routes sur le multiplexeur.
func (p *Pages) Register(mux *http.ServeMux) {
	for _, r := range p.Routes() {
		mux.Handle(r.Pattern, r.Handler)
	}
}

// ---------------------------------------------------------------- commun

type session struct {
	g     *groups.Group
	actor participants.Actor
}

// who résout le groupe et la personne connectée. Groupe inconnu : 404 ;
// pas de session : redirection vers la connexion (aucune donnée n'est donnée).
func (p *Pages) who(w http.ResponseWriter, r *http.Request) (session, bool) {
	g, err := p.o.Groups.BySlug(r.Context(), r.PathValue("slug"))
	if err != nil || g == nil {
		notFound(w)
		return session{}, false
	}
	sess, found := websec.ParticipantSession(r.Context(), strconv.FormatInt(g.ID, 10))
	if !found {
		http.Redirect(w, r, "/"+g.Slug+"/connexion", http.StatusSeeOther)
		return session{}, false
	}
	pid, err := strconv.ParseInt(sess.Subject, 10, 64)
	if err != nil || pid <= 0 {
		http.Redirect(w, r, "/"+g.Slug+"/connexion", http.StatusSeeOther)
		return session{}, false
	}
	return session{g: g, actor: participants.ParticipantActor(g.ID, pid)}, true
}

func notFound(w http.ResponseWriter) {
	w.Header().Set("Cache-Control", "no-store")
	http.Error(w, "404 page not found", http.StatusNotFound)
}

func (p *Pages) render(w http.ResponseWriter, r *http.Request, status int, name string, d any) {
	clone, err := p.tpl[name].Clone()
	if err != nil {
		http.Error(w, "Internal error", http.StatusInternalServerError)
		return
	}
	clone.Funcs(websec.TemplateFuncs(r)).Funcs(i18nweb.Funcs(p.o.Translator, r))
	var buf bytes.Buffer
	if err := clone.ExecuteTemplate(&buf, "layout/base", d); err != nil {
		http.Error(w, "Internal error", http.StatusInternalServerError)
		return
	}
	h := w.Header()
	h.Set("Content-Type", "text/html; charset=utf-8")
	h.Set("Cache-Control", "private, no-store")
	h.Set("Referrer-Policy", "no-referrer")
	w.WriteHeader(status)
	_, _ = w.Write(buf.Bytes())
}

func (p *Pages) lang(r *http.Request) string {
	return negotiate.Lang(r.Context(), p.o.Translator.DefaultLang())
}

// ideaView est une idée prête à afficher.
type ideaView struct {
	ID           int64
	Title        string
	URL          string
	URLLabel     string
	Text         string
	ImageSrc     string
	DeleteAction string
}

// linkOf ne garde que les liens http(s) ; le libellé est l'hôte et le chemin.
func linkOf(raw string) (href, label string) {
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return "", ""
	}
	label = u.Host + strings.TrimRight(u.EscapedPath(), "/")
	return raw, label
}

// ---------------------------------------------------------------- écriture

type writeData struct {
	Lang, TitleKey string
	Slug           string
	BackURL        string
	Action         string
	IdeaAction     string
	Welcome        bool
	Intro          string
	SavedAt        string
	Ideas          []ideaView
	Count          int
	MaxReached     bool
	IntroError     bool
	IdeaError      bool
	InternalError  bool
	IdeaTitle      string
	IdeaURL        string
	IdeaText       string
}

func (p *Pages) writeData(r *http.Request, s session, l *letter.Letter) writeData {
	d := writeData{
		Lang: p.lang(r), TitleKey: "letter.title.write", Slug: s.g.Slug,
		BackURL:    p.o.MailboxURL(s.g.Slug),
		Action:     "/" + s.g.Slug + "/lettre",
		IdeaAction: "/" + s.g.Slug + "/lettre/idees",
		Intro:      l.Intro, Count: len(l.Ideas), MaxReached: len(l.Ideas) >= letter.MaxIdeas,
	}
	if l.Version > 0 && !l.UpdatedAt.IsZero() {
		layout := "15:04"
		if d.Lang == "fr" {
			layout = "15 h 04" // maquette : « Enregistré à 14 h 32 »
		}
		d.SavedAt = l.UpdatedAt.In(p.o.Location).Format(layout)
	}
	for _, i := range l.Ideas {
		v := ideaView{ID: i.ID, Title: i.Title, Text: i.Text,
			DeleteAction: fmt.Sprintf("/%s/lettre/idees/%d/supprimer", s.g.Slug, i.ID)}
		v.URL, v.URLLabel = linkOf(i.URL)
		if i.HasImage {
			v.ImageSrc = fmt.Sprintf("/%s/lettre/image/%d", s.g.Slug, i.ID)
		}
		d.Ideas = append(d.Ideas, v)
	}
	return d
}

func (p *Pages) own(r *http.Request, s session) (*letter.Letter, error) {
	return p.o.Letters.Get(r.Context(), s.g.ID, s.actor.ParticipantID)
}

// WritePage rend screen-lettre (GET /{slug}/lettre[?bienvenue=1]). Le
// paramètre ne change aucun droit : il affiche le bandeau et le focus.
func (p *Pages) WritePage() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s, ok := p.who(w, r)
		if !ok {
			return
		}
		l, err := p.own(r, s)
		if err != nil {
			p.failWrite(w, r, s, err)
			return
		}
		d := p.writeData(r, s, l)
		d.Welcome = r.URL.Query().Get("bienvenue") == "1"
		p.render(w, r, http.StatusOK, tplWrite, d)
	})
}

func (p *Pages) failWrite(w http.ResponseWriter, r *http.Request, s session, err error) {
	if errs.Is(err, errs.KindNotFound) {
		notFound(w)
		return
	}
	d := writeData{Lang: p.lang(r), TitleKey: "letter.title.write", Slug: s.g.Slug,
		BackURL: p.o.MailboxURL(s.g.Slug), Action: "/" + s.g.Slug + "/lettre",
		IdeaAction: "/" + s.g.Slug + "/lettre/idees", InternalError: true}
	p.render(w, r, http.StatusInternalServerError, tplWrite, d)
}

func (p *Pages) redirectWrite(w http.ResponseWriter, r *http.Request, s session) {
	http.Redirect(w, r, "/"+s.g.Slug+"/lettre", http.StatusSeeOther)
}

// SaveIntro enregistre le message (POST /{slug}/lettre).
func (p *Pages) SaveIntro() http.Handler {
	return websec.RequireUnsafeMethod(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s, ok := p.who(w, r)
		if !ok {
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, maxIntroForm)
		if r.ParseForm() != nil {
			p.rerender(w, r, s, func(d *writeData) { d.IntroError = true })
			return
		}
		intro := r.PostForm.Get("intro")
		if _, err := p.o.Letters.SetIntro(r.Context(), s.g.ID, s.actor.ParticipantID, intro); err != nil {
			if errs.Is(err, errs.KindValidation) {
				p.rerender(w, r, s, func(d *writeData) { d.IntroError = true; d.Intro = intro })
				return
			}
			p.failWrite(w, r, s, err)
			return
		}
		p.redirectWrite(w, r, s)
	}))
}

// rerender réaffiche la page avec une erreur intégrée (statut 422).
func (p *Pages) rerender(w http.ResponseWriter, r *http.Request, s session, mod func(*writeData)) {
	l, err := p.own(r, s)
	if err != nil {
		p.failWrite(w, r, s, err)
		return
	}
	d := p.writeData(r, s, l)
	mod(&d)
	p.render(w, r, http.StatusUnprocessableEntity, tplWrite, d)
}

// AddIdea ajoute une idée, photo comprise (POST /{slug}/lettre/idees).
func (p *Pages) AddIdea() http.Handler {
	return websec.RequireUnsafeMethod(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s, ok := p.who(w, r)
		if !ok {
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, maxIdeaForm)
		in := letter.IdeaInput{}
		if err := r.ParseMultipartForm(1 << 20); err != nil && !errors.Is(err, http.ErrNotMultipart) {
			p.rerender(w, r, s, func(d *writeData) { d.IdeaError = true })
			return
		}
		title, link, text := r.FormValue("title"), r.FormValue("url"), r.FormValue("text")
		in.Title, in.URL, in.Text = title, link, text
		if f, _, err := r.FormFile("image"); err == nil {
			buf := new(bytes.Buffer)
			_, rerr := buf.ReadFrom(http.MaxBytesReader(w, f, letter.ImageMaxBytes+1))
			_ = f.Close()
			if rerr != nil {
				p.rerender(w, r, s, func(d *writeData) { d.IdeaError = true; d.IdeaTitle, d.IdeaURL, d.IdeaText = title, link, text })
				return
			}
			in.Image = buf.Bytes()
		}
		l, err := p.own(r, s)
		if err != nil {
			p.failWrite(w, r, s, err)
			return
		}
		if len(l.Ideas) >= letter.MaxIdeas {
			p.rerender(w, r, s, func(d *writeData) { d.IdeaTitle, d.IdeaURL, d.IdeaText = title, link, text })
			return
		}
		if _, err := p.o.Letters.AddIdea(r.Context(), s.g.ID, s.actor.ParticipantID, in); err != nil {
			if errs.Is(err, errs.KindValidation) {
				p.rerender(w, r, s, func(d *writeData) { d.IdeaError = true; d.IdeaTitle, d.IdeaURL, d.IdeaText = title, link, text })
				return
			}
			p.failWrite(w, r, s, err)
			return
		}
		p.redirectWrite(w, r, s)
	}))
}

// DeleteIdea supprime une idée de sa propre lettre
// (POST /{slug}/lettre/idees/{idee}/supprimer).
func (p *Pages) DeleteIdea() http.Handler {
	return websec.RequireUnsafeMethod(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s, ok := p.who(w, r)
		if !ok {
			return
		}
		id, err := strconv.ParseInt(r.PathValue("idee"), 10, 64)
		if err != nil || id <= 0 {
			notFound(w)
			return
		}
		if err := p.o.Letters.DeleteIdea(r.Context(), s.g.ID, s.actor.ParticipantID, id); err != nil {
			if errs.Is(err, errs.KindNotFound) || errs.Is(err, errs.KindValidation) {
				notFound(w)
				return
			}
			p.failWrite(w, r, s, err)
			return
		}
		p.redirectWrite(w, r, s)
	}))
}

// OwnImage sert l'image d'une idée de la lettre de la personne connectée
// (GET /{slug}/lettre/image/{idee}) ; jamais celle d'une autre lettre.
func (p *Pages) OwnImage() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s, ok := p.who(w, r)
		if !ok {
			return
		}
		id, err := strconv.ParseInt(r.PathValue("idee"), 10, 64)
		if err != nil || id <= 0 {
			notFound(w)
			return
		}
		l, err := p.own(r, s)
		if err != nil {
			notFound(w)
			return
		}
		mine := false
		for _, i := range l.Ideas {
			if i.ID == id && i.HasImage {
				mine = true
			}
		}
		if !mine {
			notFound(w)
			return
		}
		b, mime, err := p.o.Letters.ImageFile(r.Context(), s.g.ID, id)
		if err != nil {
			notFound(w)
			return
		}
		h := w.Header()
		h.Set("Content-Type", mime)
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Cache-Control", "private, no-store")
		h.Set("Content-Length", strconv.Itoa(len(b)))
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(b)
	})
}

// ---------------------------------------------------------------- lecture

type tabView struct {
	URL     string
	N       int
	Total   int
	Name    string
	Current bool
}

type receivedData struct {
	Lang, TitleKey string
	Slug           string
	BackURL        string
	Name           string
	Intro          string
	Ideas          []ideaView
	Tabs           []tabView
	// Budget est vide quand le groupe n'a pas de budget : aucune mention.
	Budget string
}

// ReceivedPage rend screen-lettre-recue (GET /{slug}/lettre-recue/{cible}),
// en lecture seule. Tout refus (cible d'un autre, non découverte, avant la
// révélation) donne le même 404.
func (p *Pages) ReceivedPage() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s, ok := p.who(w, r)
		if !ok {
			return
		}
		pairID, err := strconv.ParseInt(r.PathValue("cible"), 10, 64)
		if err != nil || pairID <= 0 {
			notFound(w)
			return
		}
		l, err := p.o.Reader.Read(r.Context(), s.actor, s.g.ID, pairID)
		if err != nil {
			if errs.Is(err, errs.KindInternal) {
				http.Error(w, "Internal error", http.StatusInternalServerError)
				return
			}
			notFound(w)
			return
		}
		mine, err := p.o.Targets.MyTargets(r.Context(), s.actor, s.g.ID)
		if err != nil || mine == nil {
			notFound(w)
			return
		}
		d := receivedData{Lang: p.lang(r), TitleKey: "letter.title.received", Slug: s.g.Slug,
			BackURL: p.o.HomeURL(s.g.Slug), Intro: l.Intro}
		total := len(mine.Targets)
		n := 0
		for _, t := range mine.Targets {
			n++
			if !t.Discovered {
				continue
			}
			name, err := p.o.Names.FirstName(r.Context(), s.g.ID, t.ReceiverID)
			if err != nil {
				continue
			}
			if t.PairID == pairID {
				d.Name = name
			}
			if total > 1 {
				d.Tabs = append(d.Tabs, tabView{
					URL: fmt.Sprintf("/%s/lettre-recue/%d", s.g.Slug, t.PairID),
					N:   n, Total: total, Name: name, Current: t.PairID == pairID})
			}
		}
		if d.Name == "" {
			notFound(w)
			return
		}
		// Le budget est relu à chaque affichage : un changement ou un retrait
		// se voit aussitôt, sans toucher à la lettre ni à sa version.
		if g, err := p.o.Groups.BySlug(r.Context(), s.g.Slug); err == nil && g != nil {
			d.Budget = g.BudgetLabel()
		}
		for _, i := range l.Ideas {
			v := ideaView{ID: i.ID, Title: i.Title, Text: i.Text}
			v.URL, v.URLLabel = linkOf(i.URL)
			if i.HasImage {
				v.ImageSrc = strings.NewReplacer("{slug}", s.g.Slug,
					"{cible}", strconv.FormatInt(pairID, 10), "{idee}", strconv.FormatInt(i.ID, 10)).Replace(letterread.ImagePath)
			}
			d.Ideas = append(d.Ideas, v)
		}
		p.render(w, r, http.StatusOK, tplReceived, d)
	})
}
