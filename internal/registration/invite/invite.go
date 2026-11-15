// Package invite porte le mode « inscription par lien » (F11.1) et le
// service du formulaire d'inscription (F11.2) : jeton d'invitation de 128
// bits distinct du slug, stocké haché et comparé à temps constant ;
// renouvellement ; plafond de places ; date de clôture ; fermeture dès que le
// tirage est fait ; validations, champ piège, unicité du nom par la règle de
// F3 et adresse partagée confirmée.
//
// Le code de vérification (F11.3), la vérification réussie (F11.4) et le
// magasin SQL (F11.6) se branchent par les ports CodeIssuer, Throttle et
// Store.
package invite

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"errors"
	"log/slog"
	"sync"
	"time"

	"secretsanta/internal/groups"
	"secretsanta/internal/participants"
	"secretsanta/internal/shared/clock"
	"secretsanta/internal/shared/errs"
	"secretsanta/internal/shared/ports"
	"secretsanta/internal/shared/random"
)

// PendingTTL est la durée de vie d'une inscription en attente.
const PendingTTL = 48 * time.Hour

// MaxCapacity borne le plafond de places réglable.
const MaxCapacity = 1000

// constantTimeEq compare deux condensés à temps constant. Variable de paquet
// pour que les tests observent ses appels.
var constantTimeEq = subtle.ConstantTimeCompare

// ErrNeutral signale un slug inconnu, un mode désactivé ou un jeton absent,
// invalide ou renouvelé : la couche HTTP répond la réponse neutre de F2,
// identique dans tous ces cas.
var ErrNeutral = errors.New("invite: réponse neutre")

// Groups résout un groupe ; satisfait par *groups.Service. Un groupe inconnu
// renvoie une erreur de nature errs.KindNotFound.
type Groups interface {
	BySlug(ctx context.Context, slug string) (*groups.Group, error)
	ByID(ctx context.Context, id int64) (*groups.Group, error)
}

// CodeIssuer émet le code de vérification d'une inscription en attente et
// l'envoie à son adresse (F11.3). Une erreur annule l'inscription.
type CodeIssuer interface {
	IssueCode(ctx context.Context, p Pending) error
}

// Throttle applique les plafonds d'inscription (F11.3). Facultatif.
//
// AllowSubmission compte chaque soumission valide au format, avant les
// contrôles de nom et d'adresse qui peuvent révéler un nom ou une adresse
// déjà connus : sans lui, un détenteur du lien pourrait les sonder sans
// limite (par exemple 20 par fenêtre de 15 minutes et par adresse IP).
// AllowRegistration ne compte que les créations, juste avant l'ajout de
// l'inscription en attente (par exemple 5 par fenêtre de 15 minutes et par
// adresse IP) ; deux compteurs distincts, pour qu'une personne qui corrige
// une faute ne brûle pas ses créations.
type Throttle interface {
	AllowSubmission(ctx context.Context, groupID int64, ip string) error
	AllowRegistration(ctx context.Context, groupID int64, ip, emailKey string) error
}

// Config rassemble les dépendances du service.
type Config struct {
	Groups    Groups
	Directory Directory
	Store     Store
	Draw      ports.DrawState
	Clock     clock.Clock
	// Key est la clé HMAC des jetons (SIGNING_SECRET).
	Key      []byte
	Codes    CodeIssuer
	Throttle Throttle
	Logger   *slog.Logger
}

// Service gère le mode d'inscription et les soumissions du formulaire.
type Service struct {
	cfg   Config
	dummy []byte
	// mu sérialise contrôle et création d'une inscription (plafond, unicité).
	mu sync.Mutex
}

// New construit le service.
func New(cfg Config) (*Service, error) {
	if cfg.Groups == nil || cfg.Directory == nil || cfg.Store == nil || cfg.Draw == nil ||
		cfg.Clock == nil || cfg.Codes == nil {
		return nil, errors.New("invite: dépendance manquante")
	}
	if len(cfg.Key) < 16 {
		return nil, errors.New("invite: clé HMAC trop courte")
	}
	cfg.Key = append([]byte(nil), cfg.Key...)
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}
	d, err := random.Bytes(sha256.Size)
	if err != nil {
		return nil, err
	}
	return &Service{cfg: cfg, dummy: d}, nil
}

func (s *Service) hash(token string) []byte {
	m := hmac.New(sha256.New, s.cfg.Key)
	m.Write([]byte("registration-invite:"))
	m.Write([]byte(token))
	return m.Sum(nil)
}

// Link est le lien d'invitation, renvoyé une seule fois (activation ou
// renouvellement) : le jeton n'est stocké que haché.
type Link struct {
	Token string
	// Path vaut /<slug>/rejoindre/<jeton>.
	Path string
}

// Settings est la vue d'administration du mode (sans jeton).
type Settings struct {
	Enabled  bool
	HasToken bool
	Capacity int
	ClosesAt *time.Time
	// RoleChoice : les inscrits choisissent leur rôle (F11d).
	RoleChoice bool
}

// Messages français.
const (
	msgAuth           = "Authentification requise."
	msgForbidden      = "Vous n'avez pas le droit d'effectuer cette action."
	msgRenewDisabled  = "Activez d'abord l'inscription par lien."
	msgCapacity       = "Le nombre de places doit être compris entre 1 et 1000."
	msgClosedFull     = "Les inscriptions sont closes : toutes les places sont prises."
	msgClosedDate     = "Les inscriptions sont closes : la date de clôture est passée."
	msgClosedDraw     = "Les inscriptions sont closes : le tirage est fait. Demandez à l'organisateur de vous ajouter avec « Ajuster le tirage »."
	codeClosedFull    = "registration.closed_full"
	codeClosedDate    = "registration.closed_date"
	codeClosedDraw    = "registration.closed_draw"
	codeRenewDisabled = "registration.renew_disabled"
	codeCapacity      = "registration.capacity_invalid"
)

// ClosedReason dit pourquoi les inscriptions sont closes ("" = ouvertes).
type ClosedReason string

const (
	ClosedFull ClosedReason = "complet"
	ClosedDate ClosedReason = "date"
	ClosedDraw ClosedReason = "tirage"
)

// AdjustDrawHint est vrai pour la fermeture au tirage : les arrivées
// tardives passent par « Ajuster le tirage ».
func (c ClosedReason) AdjustDrawHint() bool { return c == ClosedDraw }

// Message renvoie le message français et son code de catalogue.
func (c ClosedReason) Message() (code, message string) {
	switch c {
	case ClosedFull:
		return codeClosedFull, msgClosedFull
	case ClosedDate:
		return codeClosedDate, msgClosedDate
	case ClosedDraw:
		return codeClosedDraw, msgClosedDraw
	}
	return "", ""
}

// Err renvoie l'erreur typée de la fermeture (nil si ouvertes).
func (c ClosedReason) Err() error {
	if c == "" {
		return nil
	}
	code, msg := c.Message()
	return errs.Forbidden(msg).WithCode(code)
}

func requireAdmin(a participants.Actor, groupID int64) error {
	if a.Kind == participants.ActorAnonymous {
		return errs.New(errs.KindUnauthorized, msgAuth)
	}
	if a.Kind != participants.ActorAdmin || groupID == 0 || a.GroupID != groupID {
		return errs.Forbidden(msgForbidden)
	}
	return nil
}

// Settings renvoie l'état du mode pour l'administrateur du groupe.
func (s *Service) Settings(ctx context.Context, a participants.Actor, groupID int64) (Settings, error) {
	if err := requireAdmin(a, groupID); err != nil {
		return Settings{}, err
	}
	inv, err := s.cfg.Store.Invitation(ctx, groupID)
	if err != nil {
		return Settings{}, err
	}
	return Settings{Enabled: inv.Enabled, HasToken: len(inv.TokenHash) > 0, Capacity: inv.Capacity, ClosesAt: inv.ClosesAt,
		RoleChoice: inv.RoleChoice}, nil
}

// Enable active le mode et émet un nouveau jeton (l'ancien, s'il existe, ne
// vaut plus rien) ; le lien n'est renvoyé qu'ici et par Renew.
func (s *Service) Enable(ctx context.Context, a participants.Actor, groupID int64) (*Link, error) {
	return s.issue(ctx, a, groupID, false)
}

// Renew remplace le jeton en vigueur : l'ancien lien donne aussitôt la
// réponse neutre. Le mode doit être actif.
func (s *Service) Renew(ctx context.Context, a participants.Actor, groupID int64) (*Link, error) {
	return s.issue(ctx, a, groupID, true)
}

func (s *Service) issue(ctx context.Context, a participants.Actor, groupID int64, renew bool) (*Link, error) {
	if err := requireAdmin(a, groupID); err != nil {
		return nil, err
	}
	g, err := s.cfg.Groups.ByID(ctx, groupID)
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	inv, err := s.cfg.Store.Invitation(ctx, groupID)
	if err != nil {
		return nil, err
	}
	if renew && !inv.Enabled {
		return nil, errs.Validation(msgRenewDisabled).WithCode(codeRenewDisabled)
	}
	tok, err := random.Token()
	if err != nil {
		return nil, errs.Internal(err)
	}
	inv.GroupID = groupID
	inv.Enabled = true
	inv.TokenHash = s.hash(tok)
	inv.UpdatedAt = s.cfg.Clock.Now().UTC()
	if err := s.cfg.Store.SaveInvitation(ctx, inv); err != nil {
		return nil, err
	}
	return &Link{Token: tok, Path: "/" + g.Slug + "/rejoindre/" + tok}, nil
}

// Disable désactive le mode : le formulaire est aussitôt fermé (réponse
// neutre). Le condensé est effacé : réactiver émet un nouveau jeton.
func (s *Service) Disable(ctx context.Context, a participants.Actor, groupID int64) error {
	if err := requireAdmin(a, groupID); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	inv, err := s.cfg.Store.Invitation(ctx, groupID)
	if err != nil {
		return err
	}
	inv.GroupID = groupID
	inv.Enabled = false
	inv.TokenHash = nil
	inv.UpdatedAt = s.cfg.Clock.Now().UTC()
	return s.cfg.Store.SaveInvitation(ctx, inv)
}

// SetLimits règle le plafond de places (0 = aucun) et la date de clôture
// (nil = aucune).
func (s *Service) SetLimits(ctx context.Context, a participants.Actor, groupID int64, capacity int, closesAt *time.Time) error {
	if err := requireAdmin(a, groupID); err != nil {
		return err
	}
	if capacity < 0 || capacity > MaxCapacity {
		return errs.Validation(msgCapacity).WithCode(codeCapacity)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	inv, err := s.cfg.Store.Invitation(ctx, groupID)
	if err != nil {
		return err
	}
	inv.GroupID = groupID
	inv.Capacity = capacity
	inv.ClosesAt = nil
	if closesAt != nil {
		t := closesAt.UTC()
		inv.ClosesAt = &t
	}
	inv.UpdatedAt = s.cfg.Clock.Now().UTC()
	return s.cfg.Store.SaveInvitation(ctx, inv)
}

// SetRoleChoice active ou désactive l'option « Proposer aux inscrits de
// choisir leur rôle » (F11d). Modifiable à tout moment, mode actif ou non,
// avant comme après le tirage ; les inscriptions déjà en attente gardent le
// rôle retenu à leur soumission. Désactivée, toute valeur de rôle envoyée est
// ignorée et le rôle vaut normal.
func (s *Service) SetRoleChoice(ctx context.Context, a participants.Actor, groupID int64, enabled bool) error {
	if err := requireAdmin(a, groupID); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	inv, err := s.cfg.Store.Invitation(ctx, groupID)
	if err != nil {
		return err
	}
	inv.GroupID = groupID
	inv.RoleChoice = enabled
	inv.UpdatedAt = s.cfg.Clock.Now().UTC()
	return s.cfg.Store.SaveInvitation(ctx, inv)
}

// PendingView est une inscription en attente vue par l'administrateur.
type PendingView struct {
	ID        int64
	FirstName string
	Name      string
	Email     string
	CreatedAt time.Time
	ExpiresAt time.Time
}

// ListPending renvoie les inscriptions en attente non expirées du groupe de
// l'administrateur, et seulement de celui-ci.
func (s *Service) ListPending(ctx context.Context, a participants.Actor, groupID int64) ([]PendingView, error) {
	if err := requireAdmin(a, groupID); err != nil {
		return nil, err
	}
	ps, err := s.cfg.Store.ListPending(ctx, groupID, s.cfg.Clock.Now().UTC())
	if err != nil {
		return nil, err
	}
	out := make([]PendingView, 0, len(ps))
	for _, p := range ps {
		if p.GroupID != groupID {
			continue
		}
		out = append(out, PendingView{ID: p.ID, FirstName: p.FirstName, Name: p.Name, Email: p.Email,
			CreatedAt: p.CreatedAt, ExpiresAt: p.ExpiresAt})
	}
	return out, nil
}

// Invite est une invitation valide résolue depuis /<slug>/rejoindre/<jeton>.
type Invite struct {
	GroupID int64
	Slug    string
	Name    string
	// Closed est vide si les inscriptions sont ouvertes.
	Closed ClosedReason
	// RoleChoice : le formulaire propose le choix du rôle (F11d).
	RoleChoice bool
}

// resolve vérifie le slug, le mode et le jeton ; ErrNeutral dans tous les cas
// d'échec, sans dire lequel. Le jeton est toujours comparé à temps constant,
// à un condensé factice quand le mode est désactivé.
func (s *Service) resolve(ctx context.Context, slug, token string) (*groups.Group, Invitation, error) {
	if slug == "" || token == "" {
		return nil, Invitation{}, ErrNeutral
	}
	g, err := s.cfg.Groups.BySlug(ctx, slug)
	if err != nil {
		if errs.Is(err, errs.KindNotFound) {
			return nil, Invitation{}, ErrNeutral
		}
		return nil, Invitation{}, err
	}
	inv, err := s.cfg.Store.Invitation(ctx, g.ID)
	if err != nil {
		return nil, Invitation{}, err
	}
	want := inv.TokenHash
	if len(want) != sha256.Size {
		want = s.dummy
	}
	match := constantTimeEq(s.hash(token), want) == 1
	if !match || !inv.Enabled || len(inv.TokenHash) != sha256.Size {
		return nil, Invitation{}, ErrNeutral
	}
	return g, inv, nil
}

// closedReason évalue la fermeture : tirage fait, date de clôture passée,
// puis plafond (participants et inscriptions en attente non expirées).
func (s *Service) closedReason(ctx context.Context, groupID int64, inv Invitation, now time.Time) (ClosedReason, error) {
	done, err := s.cfg.Draw.IsDrawDone(ctx, groupID)
	if err != nil {
		return "", err
	}
	if done {
		return ClosedDraw, nil
	}
	if inv.ClosesAt != nil && !now.Before(*inv.ClosesAt) {
		return ClosedDate, nil
	}
	if inv.Capacity > 0 {
		n, err := s.cfg.Directory.Count(ctx, groupID)
		if err != nil {
			return "", err
		}
		p, err := s.cfg.Store.CountPending(ctx, groupID, now)
		if err != nil {
			return "", err
		}
		if n+p >= inv.Capacity {
			return ClosedFull, nil
		}
	}
	return "", nil
}

// Open résout une invitation et dit si les inscriptions sont ouvertes.
// ErrNeutral si le slug, le mode ou le jeton ne conviennent pas.
func (s *Service) Open(ctx context.Context, slug, token string) (*Invite, error) {
	g, inv, err := s.resolve(ctx, slug, token)
	if err != nil {
		return nil, err
	}
	c, err := s.closedReason(ctx, g.ID, inv, s.cfg.Clock.Now().UTC())
	if err != nil {
		return nil, err
	}
	return &Invite{GroupID: g.ID, Slug: g.Slug, Name: g.Name, Closed: c, RoleChoice: inv.RoleChoice}, nil
}

// VerificationAllowed dit si une inscription en attente du groupe peut encore
// être vérifiée : refusée (renvoi vers « Ajuster le tirage ») dès que le
// tirage est fait. À appeler par la vérification (F11.3, F11.4).
func (s *Service) VerificationAllowed(ctx context.Context, groupID int64) error {
	done, err := s.cfg.Draw.IsDrawDone(ctx, groupID)
	if err != nil {
		return err
	}
	if done {
		return ClosedDraw.Err()
	}
	return nil
}
