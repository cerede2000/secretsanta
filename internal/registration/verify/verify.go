// Package verify porte la vérification réussie d'une inscription par lien
// (F11.4) : création du participant par le port CreateVerified de F3d,
// courriel « bienvenue » de F6, purge des inscriptions en attente après 48
// heures, port RegistrationState de F1, et actions de l'administrateur
// « Valider sans code », « Renvoyer le code » et « Supprimer ».
//
// Une inscription en attente (invite.Pending) n'est pas un participant : elle
// n'a ni lien ni code personnel et n'est pas éligible au tirage. Elle ne le
// devient que par ce paquet, et toujours par CreateVerified : aucune règle de
// nom n'est recopiée ici.
package verify

import (
	"context"
	"crypto/subtle"
	"errors"
	"log/slog"
	"sync"
	"time"

	"secretsanta/internal/enrolment"
	"secretsanta/internal/groups"
	"secretsanta/internal/participants"
	"secretsanta/internal/registration/code"
	"secretsanta/internal/registration/invite"
	"secretsanta/internal/shared/clock"
	"secretsanta/internal/shared/errs"
	"secretsanta/internal/shared/ports"
	"secretsanta/internal/shared/scheduler"
)

// Codes est la part du service des codes de vérification (F11.3) utilisée
// ici ; *code.Service la satisfait.
type Codes interface {
	Issue(ctx context.Context, req code.Request) (code.Issued, error)
	Verify(ctx context.Context, groupID, registrationID int64, input string) (code.Result, error)
	Preview(ctx context.Context, groupID int64, tok string) (code.Preview, error)
	Confirm(ctx context.Context, groupID int64, tok string) (code.Result, error)
	Invalidate(ctx context.Context, groupID, registrationID int64) error
}

// Enroller est le port CreateVerified de F3d ; *enrolment.Service le
// satisfait.
type Enroller interface {
	CreateVerified(ctx context.Context, groupID int64, in participants.Input, verifiedAt time.Time) (*enrolment.Created, error)
}

// Groups résout un groupe par son identifiant ; *groups.Service le satisfait.
type Groups interface {
	ByID(ctx context.Context, id int64) (*groups.Group, error)
}

// GroupLanguage donne la langue par défaut d'un groupe (facultatif) ;
// *groupsettings.Service la satisfait.
type GroupLanguage interface {
	GroupLanguage(ctx context.Context, slug string) (string, bool)
}

// Scheduler planifie la purge d'une inscription à son expiration ;
// *scheduler.Scheduler le satisfait.
type Scheduler interface {
	At(at time.Time, t scheduler.Task) uint64
}

// Config rassemble les dépendances du service.
type Config struct {
	// Pending est le magasin des inscriptions en attente (F11.1, F11.6).
	Pending invite.Store
	// Store conserve les traces de vérification, les inscriptions ignorées et
	// le journal des actions de l'administrateur.
	Store  Store
	Codes  Codes
	Enrol  Enroller
	Groups Groups
	Draw   ports.DrawState
	Clock  clock.Clock
	// Welcome envoie le courriel « bienvenue » de F6.
	Welcome WelcomeMailer
	// Scheduler (facultatif) purge chaque inscription à son expiration.
	Scheduler Scheduler
	// Lang (facultatif) donne la langue du groupe pour les courriels.
	Lang   GroupLanguage
	Logger *slog.Logger
}

// Service vérifie les inscriptions et implémente ports.RegistrationState.
type Service struct {
	cfg Config
	// mu sérialise vérification, validation, renvoi, suppression et
	// résolution : une inscription ne devient jamais deux participants.
	mu sync.Mutex
}

var _ ports.RegistrationState = (*Service)(nil)
var _ invite.CodeIssuer = (*Service)(nil)

// New construit le service ; Scheduler, Lang et Logger sont facultatifs.
func New(cfg Config) (*Service, error) {
	if cfg.Pending == nil || cfg.Store == nil || cfg.Codes == nil || cfg.Enrol == nil ||
		cfg.Groups == nil || cfg.Draw == nil || cfg.Clock == nil || cfg.Welcome == nil {
		return nil, errors.New("verify: dépendance manquante")
	}
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}
	return &Service{cfg: cfg}, nil
}

// Success est une vérification réussie : la suite est l'écran de réussite
// (screen-inscription-reussie), dont le bouton principal mène à la page
// d'écriture de la lettre avec bienvenue=1. Elle ne contient aucune paire ni
// aucun secret.
type Success struct {
	GroupID        int64
	Slug           string
	RegistrationID int64
	ParticipantID  int64
	FirstName      string
	VerifiedAt     time.Time
	// AlreadyVerified est vrai quand l'inscription l'était déjà (par exemple
	// validée par l'administrateur) : aucun participant n'a été créé.
	AlreadyVerified bool
}

// Messages français.
const (
	msgAuth      = "Authentification requise."
	msgForbidden = "Vous n'avez pas le droit d'effectuer cette action."
	msgNotFound  = "Cette inscription n'existe pas ou a expiré."
	msgIgnored   = "Cette inscription est fermée : elle a été ignorée avant le tirage."
	msgResolve   = "Choix inconnu : « ignorer » ou « supprimer »."
	actorAdmin   = "administrateur"

	codeNotFound = "registration.pending_not_found"
	codeIgnored  = "registration.pending_ignored"
	codeResolve  = "registration.resolve_invalid"
)

func requireAdmin(a participants.Actor, groupID int64) error {
	if a.Kind == participants.ActorAnonymous {
		return errs.New(errs.KindUnauthorized, msgAuth)
	}
	if a.Kind != participants.ActorAdmin || groupID <= 0 || a.GroupID != groupID {
		return errs.Forbidden(msgForbidden)
	}
	return nil
}

// livePending renvoie l'inscription non expirée du groupe qui vérifie match.
func (s *Service) livePending(ctx context.Context, groupID int64, now time.Time, match func(invite.Pending) bool) (invite.Pending, bool, error) {
	ps, err := s.cfg.Pending.ListPending(ctx, groupID, now)
	if err != nil {
		return invite.Pending{}, false, err
	}
	for _, p := range ps {
		if p.GroupID == groupID && p.ExpiresAt.After(now) && match(p) {
			return p, true, nil
		}
	}
	return invite.Pending{}, false, nil
}

func byID(id int64) func(invite.Pending) bool {
	return func(p invite.Pending) bool { return p.ID == id }
}

func byRef(ref string) func(invite.Pending) bool {
	return func(p invite.Pending) bool {
		return ref != "" && len(p.Ref) == len(ref) && subtle.ConstantTimeCompare([]byte(p.Ref), []byte(ref)) == 1
	}
}

// fresh rapporte si la trace existe et n'a pas expiré.
func fresh(o Outcome, ok bool, now time.Time) bool { return ok && o.ExpiresAt.After(now) }

func (s *Service) isIgnored(ctx context.Context, groupID, registrationID int64) (bool, error) {
	ig, err := s.cfg.Store.Ignored(ctx, groupID)
	if err != nil {
		return false, err
	}
	return ig[registrationID], nil
}

// refuse renvoie le refus uniforme du code de vérification. Il passe par
// Codes.Verify sur une inscription qui n'existe jamais (identifiant 0) : la
// réponse est exactement celle d'un code faux, expiré ou épuisé.
func (s *Service) refuse(ctx context.Context, groupID int64, input string) error {
	_, err := s.cfg.Codes.Verify(ctx, groupID, 0, input)
	if err == nil {
		return errs.Internal(errors.New("verify: refus attendu"))
	}
	return err
}

// gate refuse toute vérification dès que le tirage est fait (renvoi vers
// « Ajuster le tirage »).
func (s *Service) gate(ctx context.Context, groupID int64) error {
	done, err := s.cfg.Draw.IsDrawDone(ctx, groupID)
	if err != nil {
		return err
	}
	if done {
		return invite.ClosedDraw.Err()
	}
	return nil
}

func already(o Outcome, slug, first string) *Success {
	return &Success{GroupID: o.GroupID, Slug: slug, RegistrationID: o.RegistrationID, ParticipantID: o.ParticipantID,
		FirstName: first, VerifiedAt: o.VerifiedAt, AlreadyVerified: true}
}

func (s *Service) slug(ctx context.Context, groupID int64) string {
	g, err := s.cfg.Groups.ByID(ctx, groupID)
	if err != nil {
		return ""
	}
	return g.Slug
}

// VerifyCode vérifie le code saisi pour l'inscription de référence ref (la
// référence remise par le formulaire). Réussite : le participant est créé
// par CreateVerified avec joined_at à l'instant de la vérification et reçoit
// le courriel « bienvenue ». Si l'inscription a déjà été vérifiée (par
// exemple validée par l'administrateur), un code juste mène à la même
// réussite, sans second participant.
//
// Une référence inconnue, une inscription expirée, ignorée ou supprimée et
// un code faux, expiré ou épuisé donnent tous le refus uniforme du code
// (code.IsRefused). Le tirage fait donne invite.ClosedDraw.
func (s *Service) VerifyCode(ctx context.Context, groupID int64, ref, input string) (*Success, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.cfg.Clock.Now().UTC()
	p, ok, err := s.livePending(ctx, groupID, now, byRef(ref))
	if err != nil {
		return nil, errs.Internal(err)
	}
	if !ok {
		o, found, err := s.cfg.Store.OutcomeByRef(ctx, groupID, ref)
		if err != nil {
			return nil, errs.Internal(err)
		}
		if !fresh(o, found, now) {
			return nil, s.refuse(ctx, groupID, input)
		}
		if _, err := s.cfg.Codes.Verify(ctx, groupID, o.RegistrationID, input); err != nil {
			return nil, err
		}
		return already(o, s.slug(ctx, groupID), ""), nil
	}
	ignored, err := s.isIgnored(ctx, groupID, p.ID)
	if err != nil {
		return nil, errs.Internal(err)
	}
	if ignored {
		return nil, s.refuse(ctx, groupID, input)
	}
	if err := s.gate(ctx, groupID); err != nil {
		return nil, err
	}
	res, err := s.cfg.Codes.Verify(ctx, groupID, p.ID, input)
	if err != nil {
		return nil, err
	}
	return s.complete(ctx, p, res.VerifiedAt.UTC(), false)
}

// PreviewLink résout le jeton du lien de confirmation en un clic pour
// afficher la page (GET) sans rien modifier. Même refus uniforme que le code
// si l'inscription n'est plus en attente (sauf déjà vérifiée).
func (s *Service) PreviewLink(ctx context.Context, groupID int64, tok string) (code.Preview, error) {
	pv, err := s.cfg.Codes.Preview(ctx, groupID, tok)
	if err != nil {
		return code.Preview{}, err
	}
	now := s.cfg.Clock.Now().UTC()
	if _, ok, err := s.livePending(ctx, groupID, now, byID(pv.RegistrationID)); err != nil {
		return code.Preview{}, errs.Internal(err)
	} else if ok {
		return pv, nil
	}
	o, found, err := s.cfg.Store.Outcome(ctx, groupID, pv.RegistrationID)
	if err != nil {
		return code.Preview{}, errs.Internal(err)
	}
	if fresh(o, found, now) {
		return pv, nil
	}
	return code.Preview{}, s.refuse(ctx, groupID, "")
}

// ConfirmLink consomme le jeton du lien de confirmation en un clic (au POST
// seulement) et vérifie l'inscription, avec les mêmes effets et refus que
// VerifyCode.
func (s *Service) ConfirmLink(ctx context.Context, groupID int64, tok string) (*Success, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	pv, err := s.cfg.Codes.Preview(ctx, groupID, tok)
	if err != nil {
		return nil, err
	}
	now := s.cfg.Clock.Now().UTC()
	p, ok, err := s.livePending(ctx, groupID, now, byID(pv.RegistrationID))
	if err != nil {
		return nil, errs.Internal(err)
	}
	if !ok {
		o, found, err := s.cfg.Store.Outcome(ctx, groupID, pv.RegistrationID)
		if err != nil {
			return nil, errs.Internal(err)
		}
		if !fresh(o, found, now) {
			return nil, s.refuse(ctx, groupID, "")
		}
		if _, err := s.cfg.Codes.Confirm(ctx, groupID, tok); err != nil {
			return nil, err
		}
		return already(o, s.slug(ctx, groupID), ""), nil
	}
	ignored, err := s.isIgnored(ctx, groupID, p.ID)
	if err != nil {
		return nil, errs.Internal(err)
	}
	if ignored {
		return nil, s.refuse(ctx, groupID, "")
	}
	if err := s.gate(ctx, groupID); err != nil {
		return nil, err
	}
	res, err := s.cfg.Codes.Confirm(ctx, groupID, tok)
	if err != nil {
		return nil, err
	}
	return s.complete(ctx, p, res.VerifiedAt.UTC(), false)
}

// complete fait de l'inscription un participant (CreateVerified de F3d),
// garde la trace de la vérification, retire l'inscription en attente et
// envoie le courriel « bienvenue ». L'appelant tient s.mu.
//
// Le code en cours n'est pas retiré : après une validation par
// l'administrateur, la personne qui saisit encore son code (valable 15
// minutes) retombe sur l'écran de réussite. La purge à l'expiration de
// l'inscription retire code et trace.
func (s *Service) complete(ctx context.Context, p invite.Pending, at time.Time, byAdmin bool) (*Success, error) {
	g, err := s.cfg.Groups.ByID(ctx, p.GroupID)
	if err != nil {
		return nil, err
	}
	created, err := s.cfg.Enrol.CreateVerified(ctx, p.GroupID, participants.Input{
		FirstName: p.FirstName, Name: p.Name, Email: p.Email, Locale: p.Lang,
		Role: p.Role,
	}, at)
	if err != nil {
		return nil, err
	}
	pid := created.Participant.ID
	if err := s.cfg.Store.SaveOutcome(ctx, Outcome{
		GroupID: p.GroupID, RegistrationID: p.ID, Ref: p.Ref, ParticipantID: pid,
		VerifiedAt: at, ByAdmin: byAdmin, ExpiresAt: p.ExpiresAt,
	}); err != nil {
		s.cfg.Logger.Error("inscription : trace de vérification non enregistrée",
			"groupe", p.GroupID, "inscription", p.ID, "erreur", err)
	}
	if err := s.cfg.Pending.DeletePending(ctx, p.GroupID, p.ID); err != nil {
		s.cfg.Logger.Error("inscription : retrait de l'inscription en attente impossible",
			"groupe", p.GroupID, "inscription", p.ID, "erreur", err)
	}
	if err := s.cfg.Store.ClearIgnored(ctx, p.GroupID, p.ID); err != nil {
		s.cfg.Logger.Error("inscription : effacement de l'état ignoré impossible",
			"groupe", p.GroupID, "inscription", p.ID, "erreur", err)
	}
	err = s.cfg.Welcome.SendWelcome(ctx, Welcome{
		GroupID: p.GroupID, ParticipantID: pid, To: p.Email, FirstName: p.FirstName,
		Lang: p.Lang, GroupLang: s.groupLang(ctx, g.Slug), GroupName: g.Name,
		Link: created.Link, Code: created.Code,
	})
	if err != nil {
		// Le participant existe : la vérification reste réussie ; le lien
		// personnel reste accessible par le lien magique de F2.
		s.cfg.Logger.Error("inscription : courriel de bienvenue non mis en file",
			"groupe", p.GroupID, "participant", pid, "erreur", err)
	}
	return &Success{GroupID: p.GroupID, Slug: g.Slug, RegistrationID: p.ID, ParticipantID: pid,
		FirstName: p.FirstName, VerifiedAt: at}, nil
}

func (s *Service) groupLang(ctx context.Context, slug string) string {
	if s.cfg.Lang == nil {
		return ""
	}
	l, ok := s.cfg.Lang.GroupLanguage(ctx, slug)
	if !ok {
		return ""
	}
	return l
}

// request construit la demande de code d'une inscription.
func (s *Service) request(ctx context.Context, g *groups.Group, p invite.Pending) code.Request {
	return code.Request{
		GroupID: p.GroupID, RegistrationID: p.ID, Slug: g.Slug, GroupName: g.Name,
		GroupLang: s.groupLang(ctx, g.Slug), To: p.Email, FirstName: p.FirstName, Lang: p.Lang,
	}
}

// IssueCode implémente invite.CodeIssuer : il émet le premier code de
// l'inscription (F11.3) et planifie sa purge à l'expiration (48 heures).
func (s *Service) IssueCode(ctx context.Context, p invite.Pending) error {
	g, err := s.cfg.Groups.ByID(ctx, p.GroupID)
	if err != nil {
		return err
	}
	if _, err := s.cfg.Codes.Issue(ctx, s.request(ctx, g, p)); err != nil {
		return err
	}
	s.schedulePurge(p.GroupID, p.ID, p.ExpiresAt)
	return nil
}
