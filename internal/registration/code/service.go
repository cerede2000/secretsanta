package code

import (
	"context"
	"errors"
	"strings"
	"sync"
	"time"

	"secretsanta/internal/access/limiter"
	"secretsanta/internal/access/token"
	"secretsanta/internal/shared/clock"
	"secretsanta/internal/shared/errs"
)

// Mail est le contenu du courriel « code de vérification ». Il porte le code
// et le lien de confirmation en clair : il n'existe que le temps de l'envoi.
type Mail struct {
	GroupID        int64
	RegistrationID int64
	To             string
	FirstName      string
	// Lang est la langue choisie par la personne (peut être vide) ;
	// GroupLang celle du groupe.
	Lang      string
	GroupLang string
	GroupName string
	Code      string
	// Link est le lien de confirmation en un clic.
	Link      string
	ExpiresAt time.Time
}

// Mailer envoie le courriel de vérification.
type Mailer interface {
	SendVerification(ctx context.Context, m Mail) error
}

// MailerFunc adapte une fonction en Mailer.
type MailerFunc func(ctx context.Context, m Mail) error

// SendVerification implémente Mailer.
func (f MailerFunc) SendVerification(ctx context.Context, m Mail) error { return f(ctx, m) }

// LinkFunc construit l'adresse absolue du lien de confirmation en un clic à
// partir du slug du groupe et du jeton (par exemple
// https://<hôte>/<slug>/inscription/confirmer/<jeton>).
type LinkFunc func(slug, tok string) string

// Options configure le service.
type Options struct {
	Store  Store
	Clock  clock.Clock
	Mailer Mailer
	Link   LinkFunc
	// Key est la clé de signature de l'instance (SIGNING_SECRET révélé).
	Key []byte
}

// Service émet et vérifie les codes de vérification d'inscription.
type Service struct {
	store  Store
	clk    clock.Clock
	mailer Mailer
	link   LinkFunc
	key    []byte

	perAddress *limiter.Limiter
	perIP      *limiter.Limiter
	perSubmit  *limiter.Limiter

	// issueMu sérialise les émissions : le délai de renvoi et le plafond par
	// adresse sont lus et écrits sans course.
	issueMu sync.Mutex
}

// New crée le service. Tous les champs des options sont obligatoires.
func New(o Options) (*Service, error) {
	switch {
	case o.Store == nil:
		return nil, errors.New("code: magasin absent")
	case o.Clock == nil:
		return nil, errors.New("code: horloge absente")
	case o.Mailer == nil:
		return nil, errors.New("code: expéditeur absent")
	case o.Link == nil:
		return nil, errors.New("code: constructeur de lien absent")
	case len(o.Key) == 0:
		return nil, token.ErrMissingKey
	}
	return &Service{
		store: o.Store, clk: o.Clock, mailer: o.Mailer, link: o.Link,
		key:        append([]byte(nil), o.Key...),
		perAddress: limiter.New(o.Clock, CodesPerAddress),
		perIP:      limiter.New(o.Clock, RegistrationsPerIP),
		perSubmit:  limiter.New(o.Clock, SubmissionsPerIP),
	}, nil
}

// AllowRegistration compte une nouvelle inscription depuis l'adresse IP dans
// le groupe et la refuse au-delà de 5 par fenêtre de 15 minutes (erreur
// enveloppant limiter.ErrLimited : répondre 429). À appeler une seule fois,
// juste avant de créer l'inscription en attente.
func (s *Service) AllowRegistration(groupID int64, ip string) error {
	if !s.perIP.Allow(limiter.IPKey(groupID, ip)).Allowed {
		return limiter.Error()
	}
	return nil
}

// AllowSubmission compte une soumission de formulaire valide depuis l'adresse
// IP dans le groupe et la refuse au-delà de 20 par fenêtre de 15 minutes
// (erreur enveloppant limiter.ErrLimited : répondre 429). À appeler une fois
// par soumission, avant tout contrôle d'adresse ou de nom : c'est le plafond
// sous lequel ces contrôles peuvent révéler quelque chose. Compteur distinct
// de celui d'AllowRegistration.
func (s *Service) AllowSubmission(groupID int64, ip string) error {
	if !s.perSubmit.Allow(limiter.IPKey(groupID, ip)).Allowed {
		return limiter.Error()
	}
	return nil
}

// Request décrit l'inscription en attente qui reçoit un code.
type Request struct {
	GroupID        int64
	RegistrationID int64
	// Slug sert au lien de confirmation ; GroupName et GroupLang au courriel.
	Slug      string
	GroupName string
	GroupLang string
	// To est l'adresse de l'inscription ; FirstName son prénom ; Lang sa
	// langue (facultative).
	To        string
	FirstName string
	Lang      string
}

// Issued décrit un code émis, sans le code lui-même (il ne part que par
// courriel).
type Issued struct {
	ExpiresAt time.Time
	// ResendAt est l'instant à partir duquel un nouveau code peut être demandé.
	ResendAt time.Time
}

// Issue émet un nouveau code pour l'inscription et envoie le courriel de
// vérification (code et lien de confirmation en un clic). Il sert à la
// première émission comme au renvoi : l'ancien code et l'ancien lien cessent
// de fonctionner et le compteur de tentatives repart de zéro.
//
// Refus : moins de 60 secondes après le code précédent (ErrTooSoon), plus de
// 3 codes en une heure pour la même adresse (limiter.ErrLimited), inscription
// déjà vérifiée (refus uniforme ErrRefused).
func (s *Service) Issue(ctx context.Context, req Request) (Issued, error) {
	if req.GroupID <= 0 || req.RegistrationID <= 0 || strings.TrimSpace(req.To) == "" ||
		strings.TrimSpace(req.FirstName) == "" || req.Slug == "" {
		return Issued{}, errs.Validation("Inscription incomplète.")
	}
	s.issueMu.Lock()
	defer s.issueMu.Unlock()

	now := s.clk.Now()
	prev, ok, err := s.store.Get(ctx, req.GroupID, req.RegistrationID)
	if err != nil {
		return Issued{}, errs.Internal(err)
	}
	if ok && prev.Consumed {
		return Issued{}, errRefused()
	}
	if ok && now.Before(prev.IssuedAt.Add(ResendDelay)) {
		return Issued{}, errTooSoon()
	}
	if !s.perAddress.Allow(limiter.TargetKey(req.GroupID, req.To)).Allowed {
		return Issued{}, limiter.Error()
	}

	tok, err := token.NewToken()
	if err != nil {
		return Issued{}, errs.Internal(err)
	}
	c := deriveCode(s.key, req.GroupID, req.RegistrationID, tok)
	rec := Record{
		GroupID: req.GroupID, RegistrationID: req.RegistrationID,
		CodeHash:  hashCode(s.key, req.GroupID, req.RegistrationID, c),
		TokenHash: hashConfirm(s.key, req.GroupID, tok),
		IssuedAt:  now, ExpiresAt: now.Add(TTL),
	}
	if err := s.store.Put(ctx, rec); err != nil {
		return Issued{}, errs.Internal(err)
	}
	err = s.mailer.SendVerification(ctx, Mail{
		GroupID: req.GroupID, RegistrationID: req.RegistrationID,
		To: req.To, FirstName: req.FirstName, Lang: req.Lang, GroupLang: req.GroupLang,
		GroupName: req.GroupName, Code: c, Link: s.link(req.Slug, tok), ExpiresAt: rec.ExpiresAt,
	})
	if err != nil {
		return Issued{}, errs.Internal(err)
	}
	return Issued{ExpiresAt: rec.ExpiresAt, ResendAt: now.Add(ResendDelay)}, nil
}

// Result est une vérification réussie.
type Result struct {
	GroupID        int64
	RegistrationID int64
	// VerifiedAt est l'instant de la vérification (joined_at du participant).
	VerifiedAt time.Time
}

// Verify vérifie le code saisi pour l'inscription (espaces, tirets et
// minuscules tolérés). Chaque saisie compte comme une tentative ; au-delà de
// 5 le code est bloqué. Un code faux, expiré, bloqué, déjà utilisé ou une
// inscription inconnue donnent exactement le même refus (ErrRefused).
func (s *Service) Verify(ctx context.Context, groupID, registrationID int64, input string) (Result, error) {
	c := token.NormalizeCode(input)
	wellFormed := token.ValidCode(c, Length)
	// L'empreinte est calculée dans tous les cas : la durée ne distingue pas
	// une saisie mal formée.
	h := hashCode(s.key, groupID, registrationID, c)

	rec, ok, err := s.store.Get(ctx, groupID, registrationID)
	if err != nil {
		return Result{}, errs.Internal(err)
	}
	if !ok || !wellFormed || rec.GroupID != groupID {
		return Result{}, errRefused()
	}
	n, ok, err := s.store.AddAttempt(ctx, groupID, registrationID, rec.CodeHash)
	if err != nil {
		return Result{}, errs.Internal(err)
	}
	now := s.clk.Now()
	match := token.Equal(rec.CodeHash, h)
	if !ok || !match || n > MaxAttempts || rec.Consumed || !now.Before(rec.ExpiresAt) {
		return Result{}, errRefused()
	}
	consumed, err := s.store.Consume(ctx, groupID, registrationID, rec.CodeHash, now, MaxAttempts)
	if err != nil {
		return Result{}, errs.Internal(err)
	}
	if !consumed {
		return Result{}, errRefused()
	}
	return Result{GroupID: groupID, RegistrationID: registrationID, VerifiedAt: now}, nil
}

// Preview est ce qu'affiche la page du lien de confirmation (GET).
type Preview struct {
	RegistrationID int64
	// Code est le code de l'inscription, affiché « déjà saisi ».
	Code      string
	ExpiresAt time.Time
}

// lookupToken résout un jeton de confirmation encore utilisable, sans rien
// modifier.
func (s *Service) lookupToken(ctx context.Context, groupID int64, tok string) (Record, string, error) {
	wellFormed := token.ValidToken(tok)
	th := hashConfirm(s.key, groupID, tok)
	rec, ok, err := s.store.FindByToken(ctx, groupID, th)
	if err != nil {
		return Record{}, "", errs.Internal(err)
	}
	now := s.clk.Now()
	if !ok || !wellFormed || rec.GroupID != groupID || !token.Equal(rec.TokenHash, th) ||
		rec.Consumed || !now.Before(rec.ExpiresAt) || rec.Attempts >= MaxAttempts {
		return Record{}, "", errRefused()
	}
	c := deriveCode(s.key, groupID, rec.RegistrationID, tok)
	if !token.Equal(rec.CodeHash, hashCode(s.key, groupID, rec.RegistrationID, c)) {
		return Record{}, "", errRefused()
	}
	return rec, c, nil
}

// Preview résout un jeton de confirmation pour afficher la page du lien en
// un clic. Il ne modifie rien : un scanner de courriel qui précharge le lien
// n'a aucun effet.
func (s *Service) Preview(ctx context.Context, groupID int64, tok string) (Preview, error) {
	rec, c, err := s.lookupToken(ctx, groupID, tok)
	if err != nil {
		return Preview{}, err
	}
	return Preview{RegistrationID: rec.RegistrationID, Code: c, ExpiresAt: rec.ExpiresAt}, nil
}

// Confirm consomme le jeton de confirmation (au POST) et vérifie
// l'inscription. Le jeton ne sert qu'une fois ; il cesse aussi de servir si
// le code a été utilisé, remplacé, a expiré ou est bloqué.
func (s *Service) Confirm(ctx context.Context, groupID int64, tok string) (Result, error) {
	rec, _, err := s.lookupToken(ctx, groupID, tok)
	if err != nil {
		return Result{}, err
	}
	now := s.clk.Now()
	consumed, err := s.store.Consume(ctx, groupID, rec.RegistrationID, rec.CodeHash, now, MaxAttempts-1)
	if err != nil {
		return Result{}, errs.Internal(err)
	}
	if !consumed {
		return Result{}, errRefused()
	}
	return Result{GroupID: groupID, RegistrationID: rec.RegistrationID, VerifiedAt: now}, nil
}

// Invalidate retire le code en cours de l'inscription : le code et le lien de
// confirmation cessent de fonctionner (suppression, purge, « ignorer »).
func (s *Service) Invalidate(ctx context.Context, groupID, registrationID int64) error {
	if err := s.store.Delete(ctx, groupID, registrationID); err != nil {
		return errs.Internal(err)
	}
	return nil
}
