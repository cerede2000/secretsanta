// Package accesspages porte les pages de connexion (F2e.3) : connexion du
// participant (code court, lien personnel, lien magique) et connexion de
// l'administrateur d'un groupe. Les gestionnaires sont construits ici et
// branchés par l'orchestrateur (voir Routes) ; aucun ne s'enregistre seul.
//
// Règles tenues par toutes les pages : réponses uniformes (aucun compte,
// adresse ni groupe n'est devinable par la réponse), liens magique et
// personnel confirmés par un bouton (GET sans effet, consommation en POST),
// tout gestionnaire modifiant l'état enveloppé par RequireUnsafeMethod,
// aucun style ni script en ligne.
package accesspages

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"html/template"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"secretsanta/internal/access/limiter"
	"secretsanta/internal/access/magic"
	"secretsanta/internal/access/token"
	"secretsanta/internal/groups"
	"secretsanta/internal/i18n/negotiate"
	i18nweb "secretsanta/internal/i18n/web"
	"secretsanta/internal/shared/clock"
	"secretsanta/internal/websec"
	"secretsanta/web"
)

// Groups est la part du service des groupes (F2) utilisée ici.
type Groups interface {
	BySlug(ctx context.Context, slug string) (*groups.Group, error)
	VerifyAdminSecret(ctx context.Context, slug, secret string) (int64, error)
}

// Credentials est la part du service de jetons (F2b) utilisée ici.
type Credentials interface {
	ResolveCode(ctx context.Context, groupID, input string) (string, error)
	ResolveLink(ctx context.Context, groupID, tok string) (string, error)
}

// MagicLinks est la part du service de lien magique (F2b) utilisée ici.
type MagicLinks interface {
	RequestLink(ctx context.Context, req magic.Request) (magic.Response, error)
	Consume(ctx context.Context, groupID int64, tok string) (int64, error)
}

// Sessions ouvre les sessions (websec.Sessions).
type Sessions interface {
	Create(ctx context.Context, w http.ResponseWriter, r *http.Request, id websec.Identity) (*websec.Session, error)
}

// AdminAttempts est la règle de tentatives de la connexion administrateur :
// 5 essais par fenêtre de 15 minutes.
var AdminAttempts = limiter.Rule{Limit: 5, Window: 15 * time.Minute}

// AdminGroupAttempts plafonne les échecs de connexion administrateur d'un
// groupe toutes adresses confondues : 50 par fenêtre de 15 minutes. Bien plus
// large que AdminAttempts pour qu'un tiers ne verrouille pas l'organisateur
// avec quelques essais ; un deviseur réparti sur plus de 50 essais peut
// encore bloquer ce seau commun (compromis assumé).
var AdminGroupAttempts = limiter.Rule{Limit: 50, Window: 15 * time.Minute}

// Options rassemble les dépendances.
type Options struct {
	Groups      Groups
	Credentials Credentials
	Magic       MagicLinks
	Sessions    Sessions
	Translator  i18nweb.Translator
	Clock       clock.Clock

	// Facultatifs.
	CodeLimiter  *limiter.Limiter // défaut : limiter.CodeAttempts
	AdminLimiter *limiter.Limiter // défaut : AdminAttempts (par adresse)
	// AdminGroupLimiter compte les échecs du groupe entier (défaut :
	// AdminGroupAttempts).
	AdminGroupLimiter *limiter.Limiter
	// ClientIP renvoie l'adresse du client (défaut : hôte de RemoteAddr ;
	// l'orchestrateur la remplace derrière un mandataire de confiance).
	ClientIP func(*http.Request) string
	// ParticipantHome et AdminHome donnent la page où aller après la connexion
	// (défauts : /<slug>/participant et /<slug>/admin).
	ParticipantHome func(slug string) string
	AdminHome       func(slug string) string
}

// Pages construit les gestionnaires.
type Pages struct {
	o     Options
	tpl   map[string]*template.Template
	codeL *limiter.Limiter
	admL  *limiter.Limiter
	admG  *limiter.Limiter
}

// Noms des gabarits (un fichier par page dans web/templates/access).
const (
	tplLogin   = "login"
	tplConfirm = "confirm"
	tplAdmin   = "admin_login"
)

// New analyse les gabarits (mise en page et sélecteur de langue compris) et
// prépare les limiteurs.
func New(o Options) (*Pages, error) {
	if o.Groups == nil || o.Credentials == nil || o.Magic == nil || o.Sessions == nil || o.Translator == nil || o.Clock == nil {
		return nil, errors.New("accesspages: dépendance manquante")
	}
	p := &Pages{o: o, tpl: map[string]*template.Template{}}
	p.codeL = o.CodeLimiter
	if p.codeL == nil {
		p.codeL = limiter.New(o.Clock, limiter.CodeAttempts)
	}
	p.admL = o.AdminLimiter
	if p.admL == nil {
		p.admL = limiter.New(o.Clock, AdminAttempts)
	}
	p.admG = o.AdminGroupLimiter
	if p.admG == nil {
		p.admG = limiter.New(o.Clock, AdminGroupAttempts)
	}
	if p.o.ClientIP == nil {
		p.o.ClientIP = remoteHost
	}
	if p.o.ParticipantHome == nil {
		p.o.ParticipantHome = func(s string) string { return "/" + s + "/participant" }
	}
	if p.o.AdminHome == nil {
		p.o.AdminHome = func(s string) string { return "/" + s + "/admin" }
	}
	funcs := template.FuncMap{}
	for k, v := range websec.TemplateFuncsPlaceholder() {
		funcs[k] = v
	}
	for k, v := range i18nweb.FuncsPlaceholder() {
		funcs[k] = v
	}
	// Chaque page définit son propre bloc "layout/content" : un jeu de
	// gabarits par page.
	for _, name := range []string{tplLogin, tplConfirm, tplAdmin} {
		t, err := template.New("").Funcs(funcs).ParseFS(web.Templates,
			"templates/layout/*.html", "templates/i18n/*.html", "templates/access/"+name+".html")
		if err != nil {
			return nil, fmt.Errorf("accesspages: gabarit %s : %w", name, err)
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

// Clés de texte des messages (module access).
const (
	keyCodeInvalid  = "access.code.invalid"
	keyEmailInvalid = "access.magic.invalid_email"
	keyLinkInvalid  = "access.error.link_invalid"
	keyLimited      = "access.error.limited"
	keyAdminInvalid = "access.admin.invalid"
	keyMagicSent    = "access.magic.sent"
	keyInternal     = "access.error.internal"
	titleLogin      = "access.title.login"
	titleAdmin      = "access.title.admin"
	titleConfirm    = "access.title.confirm"
	maxFormBytes    = 4 << 10
	pathParamSlug   = "slug"
	pathParamToken  = "token"
	formCode        = "code"
	formEmail       = "email"
	formSecret      = "secret"
	routeLogin      = "/{slug}/connexion"
	routeCode       = "/{slug}/connexion/code"
	routeMagicAsk   = "/{slug}/connexion/lien-magique"
	routeMagicLink  = "/{slug}/connexion/lien/{token}"
	routePersonal   = "/{slug}/acces/{token}"
	routeAdminLogin = "/{slug}/admin/connexion"
)

// pageData est la donnée commune des gabarits.
type pageData struct {
	Lang      string
	TitleKey  string
	Slug      string
	GroupName string
	CodeLen   int
	ErrorKey  string
	NoticeKey string
	// Action est l'URL où le formulaire de confirmation envoie son POST.
	Action string
}

func (p *Pages) render(w http.ResponseWriter, r *http.Request, status int, name string, d pageData) {
	clone, err := p.tpl[name].Clone()
	if err != nil {
		http.Error(w, "Internal error", http.StatusInternalServerError)
		return
	}
	clone.Funcs(websec.TemplateFuncs(r)).Funcs(i18nweb.Funcs(p.o.Translator, r))
	d.Lang = negotiate.Lang(r.Context(), p.o.Translator.DefaultLang())
	d.CodeLen = token.LoginCodeLength
	var buf bytes.Buffer
	if err := clone.ExecuteTemplate(&buf, "layout/base", d); err != nil {
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

// notFound est la réponse neutre d'un groupe inconnu.
func notFound(w http.ResponseWriter) {
	http.Error(w, "404 page not found", http.StatusNotFound)
}

// group retrouve le groupe du chemin ; ok est faux quand il n'existe pas.
func (p *Pages) group(r *http.Request) (*groups.Group, bool) {
	slug := r.PathValue(pathParamSlug)
	g, err := p.o.Groups.BySlug(r.Context(), slug)
	if err != nil || g == nil {
		return nil, false
	}
	return g, true
}

func gid(g *groups.Group) string { return strconv.FormatInt(g.ID, 10) }

func (p *Pages) readForm(w http.ResponseWriter, r *http.Request) bool {
	r.Body = http.MaxBytesReader(w, r.Body, maxFormBytes)
	return r.ParseForm() == nil
}

func (p *Pages) loginData(g *groups.Group) pageData {
	return pageData{TitleKey: titleLogin, Slug: g.Slug, GroupName: g.Name}
}

// LoginPage rend screen-connexion (GET /{slug}/connexion).
func (p *Pages) LoginPage() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		g, ok := p.group(r)
		if !ok {
			notFound(w)
			return
		}
		p.render(w, r, http.StatusOK, tplLogin, p.loginData(g))
	})
}

// CodeLogin vérifie le code court (POST /{slug}/connexion/code), limité à
// 10 essais par fenêtre de 15 minutes et par adresse.
func (p *Pages) CodeLogin() http.Handler {
	return websec.RequireUnsafeMethod(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		g, ok := p.group(r)
		if !ok {
			notFound(w)
			return
		}
		d := p.loginData(g)
		if !p.readForm(w, r) {
			d.ErrorKey = keyCodeInvalid
			p.render(w, r, http.StatusUnauthorized, tplLogin, d)
			return
		}
		if !p.codeL.Allow(limiter.IPKey(g.ID, p.o.ClientIP(r))).Allowed {
			d.ErrorKey = keyLimited
			p.render(w, r, http.StatusTooManyRequests, tplLogin, d)
			return
		}
		pid, err := p.o.Credentials.ResolveCode(r.Context(), gid(g), r.PostForm.Get(formCode))
		if err != nil || pid == "" {
			d.ErrorKey = keyCodeInvalid
			p.render(w, r, http.StatusUnauthorized, tplLogin, d)
			return
		}
		p.openParticipant(w, r, g, pid)
	}))
}

func (p *Pages) openParticipant(w http.ResponseWriter, r *http.Request, g *groups.Group, pid string) {
	if _, err := p.o.Sessions.Create(r.Context(), w, r, websec.Identity{Kind: websec.KindParticipant, GroupID: gid(g), Subject: pid}); err != nil {
		d := p.loginData(g)
		d.ErrorKey = keyInternal
		p.render(w, r, http.StatusInternalServerError, tplLogin, d)
		return
	}
	http.Redirect(w, r, p.o.ParticipantHome(g.Slug), http.StatusSeeOther)
}

// MagicRequest demande un lien magique (POST /{slug}/connexion/lien-magique).
// La réponse ne dépend que de la forme de l'adresse et du limiteur.
func (p *Pages) MagicRequest() http.Handler {
	return websec.RequireUnsafeMethod(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		slug := r.PathValue(pathParamSlug)
		// Un groupe inconnu reçoit la même réponse neutre (GroupID 0 : aucun
		// participant n'y est trouvé) ; la page ne montre alors pas de nom.
		var gidInt int64
		d := pageData{TitleKey: titleLogin, Slug: slug}
		if g, ok := p.group(r); ok {
			gidInt = g.ID
			d = p.loginData(g)
		}
		if !p.readForm(w, r) {
			d.ErrorKey = keyEmailInvalid
			p.render(w, r, http.StatusBadRequest, tplLogin, d)
			return
		}
		_, err := p.o.Magic.RequestLink(r.Context(), magic.Request{
			GroupID: gidInt, Slug: slug, Email: r.PostForm.Get(formEmail), IP: p.o.ClientIP(r),
		})
		switch {
		case err == nil:
			d.NoticeKey = keyMagicSent
			p.render(w, r, http.StatusOK, tplLogin, d)
		case errors.Is(err, limiter.ErrLimited):
			d.ErrorKey = keyLimited
			p.render(w, r, http.StatusTooManyRequests, tplLogin, d)
		default:
			d.ErrorKey = keyEmailInvalid
			p.render(w, r, http.StatusBadRequest, tplLogin, d)
		}
	}))
}

func (p *Pages) confirm(route string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		g, ok := p.group(r)
		if !ok {
			notFound(w)
			return
		}
		// Aucune vérification du jeton ici : un scanneur de courriel qui
		// charge le lien ne consomme rien et ne découvre rien.
		action := strings.NewReplacer("{slug}", g.Slug, "{token}", r.PathValue(pathParamToken)).Replace(route)
		d := pageData{TitleKey: titleConfirm, Slug: g.Slug, GroupName: g.Name, Action: action}
		p.render(w, r, http.StatusOK, tplConfirm, d)
	})
}

// MagicConfirm montre le bouton de confirmation du lien magique
// (GET /{slug}/connexion/lien/{token}) sans rien consommer.
func (p *Pages) MagicConfirm() http.Handler { return p.confirm(routeMagicLink) }

// PersonalConfirm montre le bouton de confirmation du lien personnel
// (GET /{slug}/acces/{token}) sans rien consommer.
func (p *Pages) PersonalConfirm() http.Handler { return p.confirm(routePersonal) }

func (p *Pages) refuseLink(w http.ResponseWriter, r *http.Request, g *groups.Group, status int, key string) {
	d := p.loginData(g)
	d.ErrorKey = key
	p.render(w, r, status, tplLogin, d)
}

// MagicConsume consomme le lien magique (POST /{slug}/connexion/lien/{token}).
func (p *Pages) MagicConsume() http.Handler {
	return websec.RequireUnsafeMethod(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		g, ok := p.group(r)
		if !ok {
			notFound(w)
			return
		}
		if !p.codeL.Allow(limiter.IPKey(g.ID, p.o.ClientIP(r))).Allowed {
			p.refuseLink(w, r, g, http.StatusTooManyRequests, keyLimited)
			return
		}
		pid, err := p.o.Magic.Consume(r.Context(), g.ID, r.PathValue(pathParamToken))
		if err != nil {
			p.refuseLink(w, r, g, http.StatusUnauthorized, keyLinkInvalid)
			return
		}
		p.openParticipant(w, r, g, strconv.FormatInt(pid, 10))
	}))
}

// PersonalConsume ouvre la session du lien personnel
// (POST /{slug}/acces/{token}). Le lien personnel reste valable.
func (p *Pages) PersonalConsume() http.Handler {
	return websec.RequireUnsafeMethod(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		g, ok := p.group(r)
		if !ok {
			notFound(w)
			return
		}
		if !p.codeL.Allow(limiter.IPKey(g.ID, p.o.ClientIP(r))).Allowed {
			p.refuseLink(w, r, g, http.StatusTooManyRequests, keyLimited)
			return
		}
		pid, err := p.o.Credentials.ResolveLink(r.Context(), gid(g), r.PathValue(pathParamToken))
		if err != nil || pid == "" {
			p.refuseLink(w, r, g, http.StatusUnauthorized, keyLinkInvalid)
			return
		}
		p.openParticipant(w, r, g, pid)
	}))
}

// AdminPage rend screen-admin-connexion (GET /{slug}/admin/connexion).
func (p *Pages) AdminPage() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		g, ok := p.group(r)
		if !ok {
			notFound(w)
			return
		}
		p.render(w, r, http.StatusOK, tplAdmin, pageData{TitleKey: titleAdmin, Slug: g.Slug, GroupName: g.Name})
	})
}

// AdminLogin vérifie le secret d'administration (POST /{slug}/admin/connexion)
// par VerifyAdminSecret et ouvre la session administrateur. Un mauvais
// secret, celui d'un autre groupe et un groupe inconnu reçoivent la même
// réponse (401, page sans nom de groupe) ; seuls les échecs comptent : au-delà
// de 5 échecs par adresse (ou 50 par groupe) et par fenêtre de 15 minutes, 429.
func (p *Pages) AdminLogin() http.Handler {
	return websec.RequireUnsafeMethod(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		slug := r.PathValue(pathParamSlug)
		var group int64
		if g, ok := p.group(r); ok {
			group = g.ID
		}
		d := pageData{TitleKey: titleAdmin, Slug: slug}
		if !p.readForm(w, r) {
			d.ErrorKey = keyAdminInvalid
			p.render(w, r, http.StatusUnauthorized, tplAdmin, d)
			return
		}
		// Seuls les échecs consomment le budget : on regarde sans compter avant
		// de vérifier, on compte après un échec.
		ipKey := limiter.IPKey(group, p.o.ClientIP(r))
		grpKey := limiter.TargetKey(group, "admin")
		if !p.admL.Peek(ipKey).Allowed || !p.admG.Peek(grpKey).Allowed {
			d.ErrorKey = keyLimited
			p.render(w, r, http.StatusTooManyRequests, tplAdmin, d)
			return
		}
		id, err := p.o.Groups.VerifyAdminSecret(r.Context(), slug, r.PostForm.Get(formSecret))
		if err != nil || id == 0 || id != group {
			p.admL.Hit(ipKey)
			p.admG.Hit(grpKey)
			d.ErrorKey = keyAdminInvalid
			p.render(w, r, http.StatusUnauthorized, tplAdmin, d)
			return
		}
		if _, err := p.o.Sessions.Create(r.Context(), w, r, websec.Identity{Kind: websec.KindAdmin, GroupID: strconv.FormatInt(id, 10)}); err != nil {
			d.ErrorKey = keyInternal
			p.render(w, r, http.StatusInternalServerError, tplAdmin, d)
			return
		}
		http.Redirect(w, r, p.o.AdminHome(slug), http.StatusSeeOther)
	}))
}

// Route décrit une route à enregistrer.
type Route struct {
	Pattern string
	Handler http.Handler
	// Modifies est vrai pour les routes qui changent l'état (POST, déjà
	// enveloppées par websec.RequireUnsafeMethod).
	Modifies bool
}

// Routes liste les routes des pages de connexion.
func (p *Pages) Routes() []Route {
	return []Route{
		{"GET " + routeLogin, p.LoginPage(), false},
		{"POST " + routeCode, p.CodeLogin(), true},
		{"POST " + routeMagicAsk, p.MagicRequest(), true},
		{"GET " + routeMagicLink, p.MagicConfirm(), false},
		{"POST " + routeMagicLink, p.MagicConsume(), true},
		{"GET " + routePersonal, p.PersonalConfirm(), false},
		{"POST " + routePersonal, p.PersonalConsume(), true},
		{"GET " + routeAdminLogin, p.AdminPage(), false},
		{"POST " + routeAdminLogin, p.AdminLogin(), true},
	}
}

// Register enregistre toutes les routes sur le multiplexeur.
func (p *Pages) Register(mux *http.ServeMux) {
	for _, r := range p.Routes() {
		mux.Handle(r.Pattern, r.Handler)
	}
}
