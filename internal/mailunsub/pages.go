package mailunsub

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"html/template"
	"net/http"

	"secretsanta/internal/groups"
	"secretsanta/internal/i18n/negotiate"
	i18nweb "secretsanta/internal/i18n/web"
	"secretsanta/internal/websec"
	"secretsanta/web"
)

// Groups est la part du service des groupes (F2) utilisée ici.
type Groups interface {
	ByID(ctx context.Context, id int64) (*groups.Group, error)
}

// PageOptions rassemble les dépendances des pages.
type PageOptions struct {
	Service    *Service
	Groups     Groups
	Translator i18nweb.Translator
}

// Pages construit les gestionnaires de screen-desinscription.
type Pages struct {
	o   PageOptions
	tpl *template.Template
}

// Routes de désinscription : le même chemin sert le lien du courriel (GET :
// confirmation seule), le bouton de la page et le POST « un clic » de
// List-Unsubscribe-Post.
const (
	RoutePattern = "/desinscription/{token}"
	pathToken    = "token"
	formAction   = "action"
	actionResub  = "resubscribe"
	maxFormBytes = 4 << 10
	tplName      = "unsubscribe"
)

// New analyse le gabarit (mise en page et sélecteur de langue compris).
func New(o PageOptions) (*Pages, error) {
	if o.Service == nil || o.Groups == nil || o.Translator == nil {
		return nil, errors.New("mailunsub: dépendance manquante")
	}
	funcs := template.FuncMap{}
	for k, v := range websec.TemplateFuncsPlaceholder() {
		funcs[k] = v
	}
	for k, v := range i18nweb.FuncsPlaceholder() {
		funcs[k] = v
	}
	t, err := template.New("").Funcs(funcs).ParseFS(web.Templates,
		"templates/layout/*.html", "templates/i18n/*.html", "templates/mail/unsubscribe.html")
	if err != nil {
		return nil, fmt.Errorf("mailunsub: gabarit : %w", err)
	}
	return &Pages{o: o, tpl: t}, nil
}

// pageData est la donnée du gabarit : les clés de texte dépendent du type.
type pageData struct {
	Lang         string
	TitleKey     string
	IntroKey     string
	SubmitKey    string
	KeepKey      string
	ResubKey     string
	FlashKey     string // état après un clic (vide : aucun)
	Unsubscribed bool
	GroupName    string
	GroupHref    string
	Action       string
}

func keysFor(typ string) (title, intro, submit, keep, resub string) {
	p := "mail.unsub." + typ + "."
	return p + "title", p + "intro", p + "submit", p + "keep", p + "resub"
}

func notFound(w http.ResponseWriter) {
	http.Error(w, "404 page not found", http.StatusNotFound)
}

func (p *Pages) data(r *http.Request, c Claims, off bool, flash string) (pageData, bool) {
	g, err := p.o.Groups.ByID(r.Context(), c.GroupID)
	if err != nil || g == nil {
		return pageData{}, false
	}
	title, intro, submit, keep, resub := keysFor(c.Type)
	return pageData{
		TitleKey: title, IntroKey: intro, SubmitKey: submit, KeepKey: keep, ResubKey: resub,
		FlashKey: flash, Unsubscribed: off, GroupName: g.Name, GroupHref: "/" + g.Slug,
		Action: RoutePrefix + r.PathValue(pathToken),
	}, true
}

func (p *Pages) render(w http.ResponseWriter, r *http.Request, d pageData) {
	clone, err := p.tpl.Clone()
	if err != nil {
		http.Error(w, "Internal error", http.StatusInternalServerError)
		return
	}
	clone.Funcs(websec.TemplateFuncs(r)).Funcs(i18nweb.Funcs(p.o.Translator, r))
	d.Lang = negotiate.Lang(r.Context(), p.o.Translator.DefaultLang())
	var buf bytes.Buffer
	if err := clone.ExecuteTemplate(&buf, "layout/base", d); err != nil {
		http.Error(w, "Internal error", http.StatusInternalServerError)
		return
	}
	h := w.Header()
	h.Set("Content-Type", "text/html; charset=utf-8")
	h.Set("Cache-Control", "no-store")
	h.Set("Referrer-Policy", "no-referrer")
	_, _ = w.Write(buf.Bytes())
}

// Page montre la confirmation (GET /desinscription/{token}). Elle ne change
// rien : un scanneur de courriel qui charge le lien ne désinscrit personne.
// Une signature fausse reçoit le 404 neutre.
func (p *Pages) Page() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := p.o.Service.Signer().Verify(r.PathValue(pathToken))
		if err != nil {
			notFound(w)
			return
		}
		off, err := p.o.Service.IsUnsubscribed(r.Context(), c.ParticipantID, c.Type)
		if err != nil {
			http.Error(w, "Internal error", http.StatusInternalServerError)
			return
		}
		flash := ""
		if off {
			flash = "mail.unsub.flash.off"
		}
		d, ok := p.data(r, c, off, flash)
		if !ok {
			notFound(w)
			return
		}
		p.render(w, r, d)
	})
}

// Submit change la préférence (POST /desinscription/{token}) : désinscription
// par défaut (corps « List-Unsubscribe=One-Click » du mail, ou bouton de la
// page), réabonnement quand action=resubscribe. Le jeton signé dans l'URL
// tient lieu de session et de jeton CSRF : la route doit être exemptée du
// CSRF (websec.CSRFOptions.Exempt). Une signature fausse reçoit le 404 neutre
// sans rien modifier.
func (p *Pages) Submit() http.Handler {
	return websec.RequireUnsafeMethod(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := p.o.Service.Signer().Verify(r.PathValue(pathToken))
		if err != nil {
			notFound(w)
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, maxFormBytes)
		_ = r.ParseForm()
		off, flash := true, "mail.unsub.flash.off"
		if r.PostForm.Get(formAction) == actionResub {
			err = p.o.Service.Resubscribe(r.Context(), c.ParticipantID, c.Type)
			off, flash = false, "mail.unsub.flash.on"
		} else {
			err = p.o.Service.Unsubscribe(r.Context(), c.GroupID, c.ParticipantID, c.Type)
		}
		if err != nil {
			http.Error(w, "Internal error", http.StatusInternalServerError)
			return
		}
		d, ok := p.data(r, c, off, flash)
		if !ok {
			notFound(w)
			return
		}
		p.render(w, r, d)
	}))
}

// IsOneClickPath dit si r est un POST de désinscription signé : c'est le
// prédicat que l'orchestrateur donne à websec.CSRFOptions.Exempt.
func IsOneClickPath(r *http.Request) bool {
	return r.Method == http.MethodPost && len(r.URL.Path) > len(RoutePrefix) &&
		r.URL.Path[:len(RoutePrefix)] == RoutePrefix
}

// Route décrit une route à enregistrer.
type Route struct {
	Pattern  string
	Handler  http.Handler
	Modifies bool
}

// Routes liste les routes de la page.
func (p *Pages) Routes() []Route {
	return []Route{
		{"GET " + RoutePattern, p.Page(), false},
		{"POST " + RoutePattern, p.Submit(), true},
	}
}

// Register enregistre les routes sur le multiplexeur.
func (p *Pages) Register(mux *http.ServeMux) {
	for _, r := range p.Routes() {
		mux.Handle(r.Pattern, r.Handler)
	}
}
