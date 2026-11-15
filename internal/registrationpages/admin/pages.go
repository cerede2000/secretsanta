package admin

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"secretsanta/internal/access/limiter"
	"secretsanta/internal/enrolment"
	"secretsanta/internal/groups"
	"secretsanta/internal/i18n/negotiate"
	"secretsanta/internal/participants"
	"secretsanta/internal/registration/code"
	"secretsanta/internal/registration/invite"
	"secretsanta/internal/registration/verify"
	"secretsanta/internal/shared/adminnav"
	"secretsanta/internal/shared/clock"
	"secretsanta/internal/shared/errs"
	"secretsanta/internal/shared/ports"
	"secretsanta/internal/websec"
)

// TemplateLayout est le gabarit à exécuter ; il inclut le bloc
// « layout/content » de web/templates/registration/admin/page.html.
const TemplateLayout = "layout/admin"

// Chemins (sous RegistrationPath, {slug} résolu par fill).
const (
	PathMode     = RegistrationPath + "/mode"
	PathRenew    = RegistrationPath + "/renouveler"
	PathLimits   = RegistrationPath + "/limites"
	PathRole     = RegistrationPath + "/choix-role"
	PathQR       = RegistrationPath + "/qr.png"
	PathValidate = RegistrationPath + "/{id}/valider"
	PathResend   = RegistrationPath + "/{id}/renvoyer"
	PathDelete   = RegistrationPath + "/{id}/supprimer"
	loginPath    = "/{slug}/admin/connexion"
	adjustPath   = "/{slug}/admin/tirage"
	maxFormBytes = 16 << 10
	dateLayout   = "2006-01-02"
)

// GroupService résout un groupe (satisfait par *groups.Service).
type GroupService interface {
	BySlug(ctx context.Context, slug string) (*groups.Group, error)
}

// InviteService est le service du mode (satisfait par *invite.Service).
type InviteService interface {
	Settings(ctx context.Context, a participants.Actor, groupID int64) (invite.Settings, error)
	Enable(ctx context.Context, a participants.Actor, groupID int64) (*invite.Link, error)
	Renew(ctx context.Context, a participants.Actor, groupID int64) (*invite.Link, error)
	Disable(ctx context.Context, a participants.Actor, groupID int64) error
	SetLimits(ctx context.Context, a participants.Actor, groupID int64, capacity int, closesAt *time.Time) error
	SetRoleChoice(ctx context.Context, a participants.Actor, groupID int64, enabled bool) error
}

// VerifyService gère les inscriptions en attente (satisfait par *verify.Service).
type VerifyService interface {
	ListPending(ctx context.Context, a participants.Actor, groupID int64) ([]verify.PendingView, error)
	ValidateWithoutCode(ctx context.Context, a participants.Actor, groupID, registrationID int64) (*verify.AdminValidation, error)
	ResendCode(ctx context.Context, a participants.Actor, groupID, registrationID int64) (code.Issued, error)
	DeleteRegistration(ctx context.Context, a participants.Actor, groupID, registrationID int64) error
}

// Counter compte les participants d'un groupe (satisfait par invite.Directory).
type Counter interface {
	Count(ctx context.Context, groupID int64) (int, error)
}

// Renderer écrit la page name avec le statut donné (fourni par l'orchestrateur).
type Renderer func(w http.ResponseWriter, r *http.Request, status int, name string, data any)

// Deps regroupe les dépendances.
type Deps struct {
	Groups   GroupService
	Invite   InviteService
	Verify   VerifyService
	Counter  Counter
	Draw     ports.DrawState
	Render   Renderer
	Nav      *adminnav.Registry
	Clock    clock.Clock
	Location *time.Location
	// PublicURL (sans barre finale) préfixe le lien d'invitation.
	PublicURL string
	// QR produit le PNG d'un QR code (enrolment.QRCodePNG si nil).
	QR          func(text string) ([]byte, error)
	DefaultLang string
}

// Row est une inscription en attente affichée.
type Row struct {
	FirstName, Name, Email string
	Minutes                int
	Hours                  int
	ExpiresHours           int
	ExpiresSoon            bool
	Ignored                bool
	Validate, Resend, Del  string
}

// Page est la donnée du gabarit.
type Page struct {
	Lang     string
	TitleKey string
	Nav      []adminnav.Item
	Slug     string
	// ErrorKey et NoticeKey sont des clés de texte (jamais du texte).
	ErrorKey  string
	NoticeKey string
	Enabled   bool
	// Closed : le tirage est fait, les inscriptions par lien sont fermées.
	Closed         bool
	Link           string
	HasQR          bool
	QRPath         string
	ConfirmRenew   bool
	Verified       int
	PendingCount   int
	Capacity       int
	ClosesAt       string
	Pending        []Row
	ModeAction     string
	RenewAction    string
	RenewCancel    string
	LimitsAction   string
	AdjustPath     string
	BackPath       string
	NextEnabledVal string
	// Option « Proposer aux inscrits de choisir leur rôle » (F11e).
	RoleChoice     bool
	RoleAction     string
	NextRoleChoice string
}

// Pages construit les gestionnaires.
type Pages struct {
	d     Deps
	mu    sync.Mutex
	links map[int64]string // dernier lien émis (jeton en clair, mémoire seulement)
}

// New valide les dépendances.
func New(d Deps) (*Pages, error) {
	if d.Groups == nil || d.Invite == nil || d.Verify == nil || d.Render == nil || d.Nav == nil || d.Clock == nil {
		return nil, errors.New("registrationpages/admin : Groups, Invite, Verify, Render, Nav et Clock sont obligatoires")
	}
	if d.DefaultLang == "" {
		d.DefaultLang = "fr"
	}
	if d.Location == nil {
		d.Location = time.UTC
	}
	if d.QR == nil {
		d.QR = enrolment.QRCodePNG
	}
	d.PublicURL = strings.TrimRight(d.PublicURL, "/")
	return &Pages{d: d, links: map[int64]string{}}, nil
}

// Register monte les routes. Les gestionnaires qui modifient l'état sont
// enveloppés par websec.RequireUnsafeMethod.
func (p *Pages) Register(mux *http.ServeMux) {
	mux.Handle("GET "+RegistrationPath, p.Page())
	mux.Handle("GET "+PathQR, p.QR())
	mux.Handle("POST "+PathMode, p.Mode())
	mux.Handle("POST "+PathRenew, p.Renew())
	mux.Handle("POST "+PathLimits, p.Limits())
	mux.Handle("POST "+PathRole, p.RoleChoice())
	mux.Handle("POST "+PathValidate, p.Validate())
	mux.Handle("POST "+PathResend, p.Resend())
	mux.Handle("POST "+PathDelete, p.Delete())
}

func fill(path, slug string) string {
	return strings.ReplaceAll(path, slugPlaceholder, slug)
}

func (p *Pages) lang(r *http.Request) string { return negotiate.Lang(r.Context(), p.d.DefaultLang) }

// admin renvoie le groupe si la session administrateur le concerne ; sinon
// il répond lui-même (404 groupe inconnu, 303 vers la connexion).
func (p *Pages) admin(w http.ResponseWriter, r *http.Request) (*groups.Group, bool) {
	g, err := p.d.Groups.BySlug(r.Context(), r.PathValue("slug"))
	if err != nil || g == nil {
		http.NotFound(w, r)
		return nil, false
	}
	if _, ok := websec.AdminSession(r.Context(), strconv.FormatInt(g.ID, 10)); !ok {
		http.Redirect(w, r, fill(loginPath, g.Slug), http.StatusSeeOther)
		return nil, false
	}
	return g, true
}

func (p *Pages) setLink(groupID int64, path string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if path == "" {
		delete(p.links, groupID)
		return
	}
	p.links[groupID] = path
}

func (p *Pages) linkPath(groupID int64) string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.links[groupID]
}

// notices : seules ces valeurs de ?ok= produisent un message.
var notices = map[string]string{
	"enabled":   "registration.admin.notice.enabled",
	"disabled":  "registration.admin.notice.disabled",
	"renewed":   "registration.admin.notice.renewed",
	"limits":    "registration.admin.notice.limits",
	"role_on":   "registration.admin.notice.role_on",
	"role_off":  "registration.admin.notice.role_off",
	"validated": "registration.admin.notice.validated",
	"resent":    "registration.admin.notice.resent",
	"deleted":   "registration.admin.notice.deleted",
}

// errorKey traduit une erreur de service en clé de texte.
func errorKey(err error) string {
	switch {
	case errors.Is(err, code.ErrTooSoon):
		return "registration.admin.err.too_soon"
	case errors.Is(err, limiter.ErrLimited):
		return "registration.admin.err.limited"
	case errs.Is(err, errs.KindNotFound):
		return "registration.admin.err.notfound"
	case errs.Is(err, errs.KindValidation):
		return "registration.admin.err.capacity"
	case errs.Is(err, errs.KindForbidden):
		return "registration.admin.err.forbidden"
	}
	return "registration.admin.err.generic"
}

func errStatus(err error) int {
	switch {
	case errors.Is(err, limiter.ErrLimited), errors.Is(err, code.ErrTooSoon):
		return http.StatusTooManyRequests
	case errs.Is(err, errs.KindNotFound):
		return http.StatusNotFound
	case errs.Is(err, errs.KindValidation):
		return http.StatusUnprocessableEntity
	case errs.Is(err, errs.KindForbidden):
		return http.StatusForbidden
	}
	return http.StatusInternalServerError
}

func (p *Pages) actor(g *groups.Group) participants.Actor { return participants.AdminActor(g.ID) }

// build assemble la page.
func (p *Pages) build(r *http.Request, g *groups.Group) (Page, error) {
	ctx := r.Context()
	a := p.actor(g)
	st, err := p.d.Invite.Settings(ctx, a, g.ID)
	if err != nil {
		return Page{}, err
	}
	pend, err := p.d.Verify.ListPending(ctx, a, g.ID)
	if err != nil {
		return Page{}, err
	}
	now := p.d.Clock.Now()
	pg := Page{
		Lang: p.lang(r), TitleKey: "registration.admin.title",
		Nav:  ResolveNav(p.d.Nav.Items(NavKey), g.Slug),
		Slug: g.Slug, Enabled: st.Enabled, Capacity: st.Capacity, PendingCount: len(pend),
		ModeAction: fill(PathMode, g.Slug), RenewAction: fill(PathRenew, g.Slug),
		LimitsAction: fill(PathLimits, g.Slug), RenewCancel: fill(RegistrationPath, g.Slug),
		AdjustPath: fill(adjustPath, g.Slug), BackPath: fill("/{slug}/admin", g.Slug),
		NextEnabledVal: "1", RoleChoice: st.RoleChoice, RoleAction: fill(PathRole, g.Slug), NextRoleChoice: "1",
	}
	if st.RoleChoice {
		pg.NextRoleChoice = "0"
	}
	if st.Enabled {
		pg.NextEnabledVal = "0"
	}
	if st.ClosesAt != nil {
		pg.ClosesAt = st.ClosesAt.Add(-time.Second).In(p.d.Location).Format(dateLayout)
	}
	if p.d.Draw != nil {
		if done, err := p.d.Draw.IsDrawDone(ctx, g.ID); err == nil {
			pg.Closed = done
		}
	}
	if p.d.Counter != nil {
		if n, err := p.d.Counter.Count(ctx, g.ID); err == nil {
			pg.Verified = n
		}
	}
	if lp := p.linkPath(g.ID); lp != "" && st.Enabled {
		pg.Link = p.d.PublicURL + lp
		pg.HasQR = true
		pg.QRPath = fill(PathQR, g.Slug)
	}
	for _, v := range pend {
		id := strconv.FormatInt(v.ID, 10)
		age := now.Sub(v.CreatedAt)
		left := v.ExpiresAt.Sub(now)
		if left < 0 {
			left = 0
		}
		row := Row{FirstName: v.FirstName, Name: v.Name, Email: v.Email,
			Minutes: int(age / time.Minute), Hours: int(age / time.Hour),
			ExpiresHours: int((left + time.Hour - 1) / time.Hour), ExpiresSoon: left < 12*time.Hour,
			Ignored: v.Ignored}
		row.Validate = strings.ReplaceAll(fill(PathValidate, g.Slug), "{id}", id)
		row.Resend = strings.ReplaceAll(fill(PathResend, g.Slug), "{id}", id)
		row.Del = strings.ReplaceAll(fill(PathDelete, g.Slug), "{id}", id)
		pg.Pending = append(pg.Pending, row)
	}
	return pg, nil
}

// Page sert l'écran (lecture seule).
func (p *Pages) Page() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		g, ok := p.admin(w, r)
		if !ok {
			return
		}
		p.show(w, r, g, http.StatusOK, "")
	})
}

func (p *Pages) show(w http.ResponseWriter, r *http.Request, g *groups.Group, status int, errKey string) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")
	pg, err := p.build(r, g)
	if err != nil {
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}
	pg.ErrorKey = errKey
	if errKey == "" {
		pg.NoticeKey = notices[r.URL.Query().Get("ok")]
		pg.ConfirmRenew = r.URL.Query().Get("confirm") == "renew" && pg.Enabled && !pg.Closed
	}
	p.d.Render(w, r, status, TemplateLayout, pg)
}

// QR sert le QR code du dernier lien émis (même origine, jamais en data:).
func (p *Pages) QR() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		g, ok := p.admin(w, r)
		if !ok {
			return
		}
		lp := p.linkPath(g.ID)
		if lp == "" {
			http.NotFound(w, r)
			return
		}
		png, err := p.d.QR(p.d.PublicURL + lp)
		if err != nil {
			http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "image/png")
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		_, _ = w.Write(png)
	})
}

// act enveloppe un gestionnaire qui change l'état : POST seulement, session
// administrateur, corps borné.
func (p *Pages) act(fn func(w http.ResponseWriter, r *http.Request, g *groups.Group) (notice string, err error)) http.Handler {
	return websec.RequireUnsafeMethod(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		g, ok := p.admin(w, r)
		if !ok {
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, maxFormBytes)
		if err := r.ParseForm(); err != nil {
			p.show(w, r, g, http.StatusBadRequest, "registration.admin.err.generic")
			return
		}
		notice, err := fn(w, r, g)
		if err != nil {
			p.show(w, r, g, errStatus(err), errorKey(err))
			return
		}
		http.Redirect(w, r, fill(RegistrationPath, g.Slug)+"?ok="+url.QueryEscape(notice), http.StatusSeeOther)
	}))
}

// Mode active ou désactive l'inscription par lien (champ enabled=1|0).
func (p *Pages) Mode() http.Handler {
	return p.act(func(w http.ResponseWriter, r *http.Request, g *groups.Group) (string, error) {
		a := p.actor(g)
		if r.PostForm.Get("enabled") == "1" {
			l, err := p.d.Invite.Enable(r.Context(), a, g.ID)
			if err != nil {
				return "", err
			}
			p.setLink(g.ID, l.Path)
			return "enabled", nil
		}
		if err := p.d.Invite.Disable(r.Context(), a, g.ID); err != nil {
			return "", err
		}
		p.setLink(g.ID, "")
		return "disabled", nil
	})
}

// RoleChoice active ou désactive l'option « Proposer aux inscrits de choisir
// leur rôle » (champ enabled=1|0), à tout moment.
func (p *Pages) RoleChoice() http.Handler {
	return p.act(func(w http.ResponseWriter, r *http.Request, g *groups.Group) (string, error) {
		on := r.PostForm.Get("enabled") == "1"
		if err := p.d.Invite.SetRoleChoice(r.Context(), p.actor(g), g.ID, on); err != nil {
			return "", err
		}
		if on {
			return "role_on", nil
		}
		return "role_off", nil
	})
}

// Renew renouvelle le lien : l'ancien cesse de fonctionner.
func (p *Pages) Renew() http.Handler {
	return p.act(func(w http.ResponseWriter, r *http.Request, g *groups.Group) (string, error) {
		l, err := p.d.Invite.Renew(r.Context(), p.actor(g), g.ID)
		if err != nil {
			return "", err
		}
		p.setLink(g.ID, l.Path)
		return "renewed", nil
	})
}

// Limits règle le plafond de places et la date de clôture (facultatifs).
func (p *Pages) Limits() http.Handler {
	return p.act(func(w http.ResponseWriter, r *http.Request, g *groups.Group) (string, error) {
		capacity := 0
		if s := strings.TrimSpace(r.PostForm.Get("capacity")); s != "" {
			n, err := strconv.Atoi(s)
			if err != nil {
				return "", errs.Validation("capacité invalide")
			}
			capacity = n
		}
		var closes *time.Time
		if s := strings.TrimSpace(r.PostForm.Get("closes_on")); s != "" {
			d, err := time.ParseInLocation(dateLayout, s, p.d.Location)
			if err != nil {
				return "", errs.Validation("date invalide")
			}
			// Inscriptions ouvertes toute la journée choisie.
			end := d.AddDate(0, 0, 1)
			closes = &end
		}
		if err := p.d.Invite.SetLimits(r.Context(), p.actor(g), g.ID, capacity, closes); err != nil {
			return "", err
		}
		return "limits", nil
	})
}

func regID(r *http.Request) (int64, error) {
	return strconv.ParseInt(r.PathValue("id"), 10, 64)
}

// Validate : « Valider sans code ».
func (p *Pages) Validate() http.Handler {
	return p.act(func(w http.ResponseWriter, r *http.Request, g *groups.Group) (string, error) {
		id, err := regID(r)
		if err != nil {
			return "", errs.NotFound("introuvable")
		}
		_, err = p.d.Verify.ValidateWithoutCode(r.Context(), p.actor(g), g.ID, id)
		return "validated", err
	})
}

// Resend : « Renvoyer le code ».
func (p *Pages) Resend() http.Handler {
	return p.act(func(w http.ResponseWriter, r *http.Request, g *groups.Group) (string, error) {
		id, err := regID(r)
		if err != nil {
			return "", errs.NotFound("introuvable")
		}
		_, err = p.d.Verify.ResendCode(r.Context(), p.actor(g), g.ID, id)
		return "resent", err
	})
}

// Delete : « Supprimer ».
func (p *Pages) Delete() http.Handler {
	return p.act(func(w http.ResponseWriter, r *http.Request, g *groups.Group) (string, error) {
		id, err := regID(r)
		if err != nil {
			return "", errs.NotFound("introuvable")
		}
		return "deleted", p.d.Verify.DeleteRegistration(r.Context(), p.actor(g), g.ID, id)
	})
}
