package adminops

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"secretsanta/internal/groups"
	"secretsanta/internal/i18n/negotiate"
	"secretsanta/internal/shared/adminnav"
	"secretsanta/internal/shared/errs"
	"secretsanta/internal/websec"
)

// Nom du gabarit exécuté (layout/admin inclut le bloc layout/content de
// web/templates/adminops/ops.html) et entrée de navigation active.
const (
	TemplateLayout = "layout/admin"
	NavKey         = "plus"
)

// Adresses des pages d'exploitation ({slug} est l'adresse du groupe).
const (
	DataPath      = "/{slug}/admin/donnees"
	ExportPath    = "/{slug}/admin/participants/export"
	ImportPath    = "/{slug}/admin/participants/import"
	BackupPath    = "/{slug}/admin/sauvegarde"
	ResetPath     = "/{slug}/admin/acces/reinitialiser"
	DuplicatePath = "/{slug}/admin/dupliquer"
	DeletePath    = "/{slug}/admin/supprimer"
	LoginPath     = "/{slug}/admin/connexion"
	MorePath      = "/{slug}/admin/plus"
)

// Vues du gabarit.
const (
	ViewData       = "data"
	ViewSecret     = "secret"
	ViewDuplicated = "duplicated"
	ViewDeleted    = "deleted"
)

// Codes de message propres aux pages.
const (
	CodeImportMissing = "adminops.error.import_missing"
	CodeGeneric       = "adminops.error.generic"
)

// GroupService est ce que les pages demandent au service des groupes.
type GroupService interface {
	BySlug(ctx context.Context, slug string) (*groups.Group, error)
}

// Ops est ce que les pages demandent au service d'exploitation (satisfait
// par *Service).
type Ops interface {
	ExportJSON(ctx context.Context, groupID int64, w io.Writer) error
	Import(ctx context.Context, groupID int64, r io.Reader) (*ImportResult, error)
	Backup(ctx context.Context, groupID int64, w io.Writer) error
	LastBackup(ctx context.Context, groupID int64) (*time.Time, error)
	ResetAdminSecret(ctx context.Context, groupID int64) (string, error)
	PlanDuplicate(ctx context.Context, groupID int64) (*DuplicatePlan, error)
	Duplicate(ctx context.Context, groupID int64) (*DuplicateResult, error)
	DeleteGroup(ctx context.Context, groupID int64, in DeleteInput) error
}

// Renderer écrit la page name avec le statut donné (fourni par l'orchestrateur).
type Renderer func(w http.ResponseWriter, r *http.Request, status int, name string, data any)

// Deps regroupe les dépendances des pages.
type Deps struct {
	Groups GroupService
	Ops    Ops
	Render Renderer
	Nav    *adminnav.Registry
	// Location sert à afficher la date de la dernière sauvegarde (UTC si nil).
	Location *time.Location
	// DefaultLang sert si la langue n'a pas été négociée (« fr » si vide).
	DefaultLang string
	// Logger reçoit les erreurs internes (slog.Default si nil).
	Logger *slog.Logger
}

// Message est un texte à afficher : clé du catalogue et paramètres (le
// gabarit passe toujours n, participants et households ; un texte n'utilise
// que les siens).
type Message struct {
	Key          string
	N            int
	Participants int
	Households   int
}

// Page est la donnée du gabarit. Elle ne contient jamais de paire du tirage.
type Page struct {
	Lang     string
	TitleKey string
	Nav      []adminnav.Item
	View     string

	Slug      string
	GroupName string
	BackURL   string

	ExportURL       string
	ImportAction    string
	BackupAction    string
	ResetAction     string
	DuplicateAction string
	DeleteAction    string

	// LastBackup est la date (AAAA-MM-JJ, fuseau de l'instance) de la
	// dernière sauvegarde, vide si aucune.
	LastBackup string
	Plan       *DuplicatePlan

	Status      *Message
	ImportError *Message
	DeleteError *Message
	// ConfirmValue est la saisie de confirmation renvoyée après un refus.
	ConfirmValue string
	Purge        bool

	// Secret est un mot de passe d'administration affiché une seule fois
	// (vues secret et duplicated).
	Secret   string
	NewName  string
	NewSlug  string
	LoginURL string
}

// Pages construit les gestionnaires.
type Pages struct{ d Deps }

// New valide les dépendances.
func New(d Deps) (*Pages, error) {
	if d.Groups == nil || d.Ops == nil || d.Render == nil || d.Nav == nil {
		return nil, errors.New("adminops : Groups, Ops, Render et Nav sont obligatoires")
	}
	if d.DefaultLang == "" {
		d.DefaultLang = "fr"
	}
	if d.Location == nil {
		d.Location = time.UTC
	}
	if d.Logger == nil {
		d.Logger = slog.Default()
	}
	return &Pages{d: d}, nil
}

// Register monte les routes sur mux (chaîne SecurityHeaders, CSRF, Sessions
// attendue en amont). Les routes qui modifient l'état sont en POST et
// enveloppées par websec.RequireUnsafeMethod.
func (p *Pages) Register(mux *http.ServeMux) {
	mux.Handle("GET "+DataPath, p.Data())
	mux.Handle("GET "+ExportPath, p.Export())
	mux.Handle("POST "+ImportPath, p.Import())
	mux.Handle("POST "+BackupPath, p.Backup())
	mux.Handle("POST "+ResetPath, p.Reset())
	mux.Handle("POST "+DuplicatePath, p.Duplicate())
	mux.Handle("POST "+DeletePath, p.Delete())
}

func fill(path, slug string) string { return strings.ReplaceAll(path, "{slug}", slug) }

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

func (p *Pages) base(r *http.Request, g *groups.Group, view string) Page {
	return Page{
		Lang: p.lang(r), TitleKey: "adminops.title", Nav: p.nav(g.Slug), View: view,
		Slug: g.Slug, GroupName: g.Name, BackURL: fill(MorePath, g.Slug),
		ExportURL: fill(ExportPath, g.Slug), ImportAction: fill(ImportPath, g.Slug),
		BackupAction: fill(BackupPath, g.Slug), ResetAction: fill(ResetPath, g.Slug),
		DuplicateAction: fill(DuplicatePath, g.Slug), DeleteAction: fill(DeletePath, g.Slug),
		LoginURL: fill(LoginPath, g.Slug),
	}
}

// dataPage complète la vue « données » (dernière sauvegarde, plan de
// duplication).
func (p *Pages) dataPage(r *http.Request, g *groups.Group) (Page, error) {
	pg := p.base(r, g, ViewData)
	last, err := p.d.Ops.LastBackup(r.Context(), g.ID)
	if err != nil {
		return pg, err
	}
	if last != nil {
		pg.LastBackup = last.In(p.d.Location).Format(groups.DateLayout)
	}
	plan, err := p.d.Ops.PlanDuplicate(r.Context(), g.ID)
	if err != nil && errs.KindOf(err) == errs.KindInternal {
		return pg, err
	}
	pg.Plan = plan
	return pg, nil
}

// message traduit une erreur en texte du catalogue : code adminops si
// présent, sinon la nature de l'erreur (core.errors.*).
func (p *Pages) message(err error) *Message {
	code, params := errs.CodeOf(err)
	if strings.HasPrefix(code, "adminops.") {
		m := &Message{Key: code}
		if n, ok := params["n"].(int); ok {
			m.N = n
		}
		return m
	}
	return &Message{Key: errs.KindKey(errs.KindOf(err))}
}

func statusOf(err error) int {
	switch errs.KindOf(err) {
	case errs.KindValidation:
		return http.StatusUnprocessableEntity
	case errs.KindConflict:
		return http.StatusConflict
	case errs.KindNotFound:
		return http.StatusNotFound
	}
	return http.StatusInternalServerError
}

func (p *Pages) fail(w http.ResponseWriter, err error) {
	p.d.Logger.Error("exploitation : erreur interne", "err", err)
	http.Error(w, "Erreur interne.", http.StatusInternalServerError)
}

// Data sert la page « Données » (lecture seule).
func (p *Pages) Data() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		g, ok := p.admin(w, r)
		if !ok {
			return
		}
		pg, err := p.dataPage(r, g)
		if err != nil {
			p.fail(w, err)
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		p.d.Render(w, r, http.StatusOK, TemplateLayout, pg)
	})
}

// Export télécharge le fichier JSON des participants (lecture seule ; jamais
// de paire du tirage).
func (p *Pages) Export() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		g, ok := p.admin(w, r)
		if !ok {
			return
		}
		var buf bytes.Buffer
		if err := p.d.Ops.ExportJSON(r.Context(), g.ID, &buf); err != nil {
			p.fail(w, err)
			return
		}
		h := w.Header()
		h.Set("Content-Type", "application/json; charset=utf-8")
		h.Set("Content-Disposition", `attachment; filename="participants-`+g.Slug+`.json"`)
		h.Set("Cache-Control", "no-store")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(buf.Bytes())
	})
}

// maxImportForm borne le corps du formulaire d'import (fichier et champs).
const maxImportForm = MaxImportBytes + 64<<10

// Import ajoute les participants d'un fichier JSON (POST multipart, champ
// « file ») ; tout ou rien.
func (p *Pages) Import() http.Handler {
	return websec.RequireUnsafeMethod(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		g, ok := p.admin(w, r)
		if !ok {
			return
		}
		pg, err := p.dataPage(r, g)
		if err != nil {
			p.fail(w, err)
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		if r.MultipartForm == nil {
			r.Body = http.MaxBytesReader(w, r.Body, maxImportForm)
		}
		file, _, ferr := r.FormFile("file")
		if ferr != nil {
			pg.ImportError = &Message{Key: CodeImportMissing}
			p.d.Render(w, r, http.StatusUnprocessableEntity, TemplateLayout, pg)
			return
		}
		defer file.Close()
		res, err := p.d.Ops.Import(r.Context(), g.ID, file)
		if err != nil {
			if errs.KindOf(err) == errs.KindInternal {
				p.fail(w, err)
				return
			}
			pg.ImportError = p.message(err)
			p.d.Render(w, r, statusOf(err), TemplateLayout, pg)
			return
		}
		pg.Status = &Message{Key: "adminops.import.done", Participants: res.Participants, Households: res.Households}
		p.d.Render(w, r, http.StatusOK, TemplateLayout, pg)
	}))
}

// Backup télécharge la sauvegarde SQLite du groupe (POST : l'opération est
// coûteuse, horodatée et ne doit pas partir d'un simple lien).
func (p *Pages) Backup() http.Handler {
	return websec.RequireUnsafeMethod(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		g, ok := p.admin(w, r)
		if !ok {
			return
		}
		var buf bytes.Buffer
		if err := p.d.Ops.Backup(r.Context(), g.ID, &buf); err != nil {
			p.fail(w, err)
			return
		}
		h := w.Header()
		h.Set("Content-Type", BackupContentType)
		h.Set("Content-Disposition", `attachment; filename="sauvegarde-`+g.Slug+`.db"`)
		h.Set("Cache-Control", "no-store")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(buf.Bytes())
	}))
}

// Reset génère un nouveau mot de passe d'administration, affiché une seule
// fois dans cette réponse ; l'ancien et les sessions sont invalidés.
func (p *Pages) Reset() http.Handler {
	return websec.RequireUnsafeMethod(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		g, ok := p.admin(w, r)
		if !ok {
			return
		}
		secret, err := p.d.Ops.ResetAdminSecret(r.Context(), g.ID)
		if err != nil {
			p.fail(w, err)
			return
		}
		pg := p.base(r, g, ViewSecret)
		pg.TitleKey = "adminops.secret.title"
		pg.Secret = secret
		w.Header().Set("Cache-Control", "no-store")
		p.d.Render(w, r, http.StatusOK, TemplateLayout, pg)
	}))
}

// Duplicate crée le groupe de l'année suivante ; son mot de passe
// d'administration est affiché une seule fois.
func (p *Pages) Duplicate() http.Handler {
	return websec.RequireUnsafeMethod(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		g, ok := p.admin(w, r)
		if !ok {
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		res, err := p.d.Ops.Duplicate(r.Context(), g.ID)
		if err != nil {
			if errs.KindOf(err) == errs.KindInternal {
				p.fail(w, err)
				return
			}
			pg, perr := p.dataPage(r, g)
			if perr != nil {
				p.fail(w, perr)
				return
			}
			pg.Status = p.message(err)
			p.d.Render(w, r, statusOf(err), TemplateLayout, pg)
			return
		}
		pg := p.base(r, g, ViewDuplicated)
		pg.TitleKey = "adminops.duplicated.title"
		pg.Secret = res.AdminSecret
		pg.NewName = res.Name
		pg.NewSlug = res.Slug
		pg.LoginURL = fill(LoginPath, res.Slug)
		p.d.Render(w, r, http.StatusOK, TemplateLayout, pg)
	}))
}

// Delete supprime le groupe après confirmation par son nom (champ
// confirm_name) ; purge=1 efface aussi les traces hors clés étrangères.
func (p *Pages) Delete() http.Handler {
	return websec.RequireUnsafeMethod(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		g, ok := p.admin(w, r)
		if !ok {
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		in := DeleteInput{ConfirmName: r.PostFormValue("confirm_name"), Purge: r.PostFormValue("purge") == "1"}
		if err := p.d.Ops.DeleteGroup(r.Context(), g.ID, in); err != nil {
			if errs.KindOf(err) == errs.KindInternal {
				p.fail(w, err)
				return
			}
			pg, perr := p.dataPage(r, g)
			if perr != nil {
				p.fail(w, perr)
				return
			}
			pg.DeleteError = p.message(err)
			pg.ConfirmValue = in.ConfirmName
			pg.Purge = in.Purge
			p.d.Render(w, r, statusOf(err), TemplateLayout, pg)
			return
		}
		p.d.Render(w, r, http.StatusOK, TemplateLayout, Page{
			Lang: p.lang(r), TitleKey: "adminops.deleted.title", View: ViewDeleted, GroupName: g.Name,
		})
	}))
}
