package groups

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"secretsanta/internal/config"
	"secretsanta/internal/shared/clock"
	"secretsanta/internal/shared/errs"
	"secretsanta/internal/shared/random"
	"secretsanta/internal/shared/sanitize"
)

// maxSecretInputBytes borne une saisie de mot de passe ou de secret avant
// condensation : au-delà, la saisie est traitée comme fausse.
const maxSecretInputBytes = 1024

// ErrTooManyAttempts est la cause des refus pour excès de tentatives ;
// l'appelant peut le tester avec errors.Is pour répondre 429.
var ErrTooManyAttempts = errors.New("groups: trop de tentatives")

// Messages d'erreur d'accès.
const (
	msgInstancePassword = "Mot de passe d'instance incorrect."
	msgTooManyAttempts  = "Trop de tentatives. Réessaie dans quelques minutes."
	msgAdminAccess      = "Accès administrateur refusé : vérifiez l'adresse du groupe et le secret d'administration."
	msgGroupNotFound    = "Ce groupe n'existe pas."
	msgMaxGiftsRange    = "Le plafond de cadeaux par personne doit être compris entre 2 et 5."
	msgMaxGiftsConfirm  = "Un plafond supérieur à 2 cadeaux par personne doit être confirmé explicitement."
	msgEventDate        = "La date du jour J n'est pas valide (format attendu : AAAA-MM-JJ)."
)

// constantTimeEq est la primitive de comparaison à temps constant. Elle
// reçoit toujours deux condensés SHA-256 de 32 octets, si bien que la durée
// ne dépend ni de la longueur ni du préfixe correct de la saisie. Variable
// de paquet pour que les tests observent ses appels.
var constantTimeEq = subtle.ConstantTimeCompare

// Service gère la création et l'accès aux groupes.
type Service struct {
	db         *sql.DB
	clock      clock.Clock
	pwDigest   [32]byte
	pwSet      bool
	dummyHash  [32]byte
	pwAttempts *attemptLimiter
}

// NewService construit le service. instancePassword protège la création ;
// vide, toute création est refusée.
func NewService(d *sql.DB, c clock.Clock, instancePassword config.Secret) (*Service, error) {
	s := &Service{
		db:         d,
		clock:      c,
		pwSet:      instancePassword.IsSet(),
		pwDigest:   sha256.Sum256([]byte(instancePassword.Reveal())),
		pwAttempts: newAttemptLimiter(c, MaxPasswordFailures, PasswordFailWindow),
	}
	b, err := random.Bytes(32)
	if err != nil {
		return nil, fmt.Errorf("aléa du service des groupes : %w", err)
	}
	copy(s.dummyHash[:], b)
	return s, nil
}

// CreateInput est la saisie de création d'un groupe.
type CreateInput struct {
	// InstancePassword est le mot de passe d'instance saisi.
	InstancePassword string
	// ClientKey identifie le client pour la limitation des tentatives
	// (adresse IP en général).
	ClientKey string

	Name string
	Slug string
	// Budget en euros, vide pour aucun budget.
	Budget           string
	GiftInstructions string
	// EventDate au format DateLayout, vide si absente.
	EventDate string
	// RevealAt est la date de révélation, nil si absente.
	RevealAt         *time.Time
	AdminCanViewPair bool
	// MaxGiftsPerGiver vaut 2 si nil. Une valeur supérieure à 2 exige
	// ConfirmMaxGifts.
	MaxGiftsPerGiver *int
	ConfirmMaxGifts  bool
}

// CreateResult est renvoyé une seule fois, à la création. AdminSecret n'est
// stocké nulle part en clair et aucun autre service ne le renvoie.
type CreateResult struct {
	Group       Group
	AdminSecret string
}

func digest(s string) [32]byte { return sha256.Sum256([]byte(s)) }

func (s *Service) instancePasswordOK(given string) bool {
	if len(given) > maxSecretInputBytes {
		given = "" // condensé tout de même calculé : durée comparable
	}
	d := digest(given)
	ok := constantTimeEq(d[:], s.pwDigest[:]) == 1
	return ok && s.pwSet && given != ""
}

// Create crée un groupe. Ordre : limitation des tentatives, mot de passe
// d'instance, puis validation des champs ; aucune information sur les
// champs n'est donnée sans le bon mot de passe.
func (s *Service) Create(ctx context.Context, in CreateInput) (*CreateResult, error) {
	if s.pwAttempts.blocked(in.ClientKey) {
		return nil, errs.Wrap(errs.KindForbidden, msgTooManyAttempts, ErrTooManyAttempts)
	}
	if !s.instancePasswordOK(in.InstancePassword) {
		s.pwAttempts.fail(in.ClientKey)
		return nil, errs.New(errs.KindUnauthorized, msgInstancePassword)
	}

	g, err := s.validate(in)
	if err != nil {
		return nil, err
	}

	secret, err := random.Token()
	if err != nil {
		return nil, errs.Internal(err)
	}
	hash := digest(secret)
	g.CreatedAt = s.clock.Now().UTC()

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, errs.Internal(err)
	}
	defer func() { _ = tx.Rollback() }()

	var n int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM groups WHERE slug = ?`, g.Slug).Scan(&n); err != nil {
		return nil, errs.Internal(err)
	}
	if n > 0 {
		return nil, errs.Conflict(msgSlugTaken)
	}

	var budget, eventDate, revealAt any
	if g.BudgetCents != nil {
		budget = *g.BudgetCents
	}
	if g.EventDate != "" {
		eventDate = g.EventDate
	}
	if g.RevealAt != nil {
		revealAt = g.RevealAt.UTC().Format(time.RFC3339)
	}
	res, err := tx.ExecContext(ctx, `INSERT INTO groups
		(slug, name, budget_cents, budget_version, gift_instructions, event_date, reveal_at,
		 admin_can_view_pair, max_gifts_per_giver, admin_secret_hash, created_at)
		VALUES (?, ?, ?, 0, ?, ?, ?, ?, ?, ?, ?)`,
		g.Slug, g.Name, budget, g.GiftInstructions, eventDate, revealAt,
		boolInt(g.AdminCanViewPair), g.MaxGiftsPerGiver, hash[:], g.CreatedAt.Format(time.RFC3339Nano))
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE") {
			return nil, errs.Conflict(msgSlugTaken)
		}
		return nil, errs.Internal(err)
	}
	if g.ID, err = res.LastInsertId(); err != nil {
		return nil, errs.Internal(err)
	}
	if err := tx.Commit(); err != nil {
		if strings.Contains(err.Error(), "UNIQUE") {
			return nil, errs.Conflict(msgSlugTaken)
		}
		return nil, errs.Internal(err)
	}
	return &CreateResult{Group: g, AdminSecret: secret}, nil
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// validate nettoie et vérifie les champs ; le slug est vérifié ici hors
// unicité.
func (s *Service) validate(in CreateInput) (Group, error) {
	var g Group
	var err error
	if g.Name, err = sanitize.Required(in.Name, NameMaxLen, "Le nom du groupe"); err != nil {
		return g, err
	}
	if g.Slug, err = ValidateSlug(in.Slug); err != nil {
		return g, err
	}
	if g.BudgetCents, err = ParseBudget(in.Budget); err != nil {
		return g, err
	}
	if g.GiftInstructions, err = sanitize.Text(in.GiftInstructions, GiftInstructionsMax, "La consigne du cadeau"); err != nil {
		return g, err
	}
	ed, err := sanitize.Line(in.EventDate, len(DateLayout), "La date du jour J")
	if err != nil {
		return g, errs.Validation(msgEventDate)
	}
	if ed != "" {
		if _, err := time.Parse(DateLayout, ed); err != nil {
			return g, errs.Validation(msgEventDate)
		}
		g.EventDate = ed
	}
	if in.RevealAt != nil {
		t := in.RevealAt.UTC().Truncate(time.Second)
		g.RevealAt = &t
	}
	g.AdminCanViewPair = in.AdminCanViewPair
	g.MaxGiftsPerGiver = DefaultMaxGiftsPerGiver
	if in.MaxGiftsPerGiver != nil {
		m := *in.MaxGiftsPerGiver
		if m < MinMaxGiftsPerGiver || m > MaxMaxGiftsPerGiver {
			return g, errs.Validation(msgMaxGiftsRange)
		}
		if m > DefaultMaxGiftsPerGiver && !in.ConfirmMaxGifts {
			return g, errs.Validation(msgMaxGiftsConfirm)
		}
		g.MaxGiftsPerGiver = m
	}
	return g, nil
}

// errAdminAccess est la réponse unique de VerifyAdminSecret : identique pour
// un groupe inconnu, un slug invalide et un secret faux.
func errAdminAccess() error { return errs.New(errs.KindUnauthorized, msgAdminAccess) }

// VerifyAdminSecret vérifie le secret d'administration du groupe slug et
// renvoie l'identifiant du groupe. La comparaison porte toujours sur deux
// condensés de 32 octets, y compris quand le groupe est inconnu (condensé
// factice), et la même erreur est renvoyée dans tous les cas d'échec.
func (s *Service) VerifyAdminSecret(ctx context.Context, slug, secret string) (int64, error) {
	var id int64
	var stored []byte
	err := s.db.QueryRowContext(ctx, `SELECT id, admin_secret_hash FROM groups WHERE slug = ?`, slug).Scan(&id, &stored)
	found := err == nil && len(stored) == sha256.Size
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return 0, errs.Internal(err)
	}
	want := s.dummyHash[:]
	if found {
		want = stored
	}
	given := secret
	if len(given) > maxSecretInputBytes {
		given = ""
	}
	d := digest(given)
	ok := constantTimeEq(d[:], want) == 1
	if !found || !ok || secret == "" {
		return 0, errAdminAccess()
	}
	return id, nil
}

const selectGroup = `SELECT id, slug, name, budget_cents, budget_version, gift_instructions,
	event_date, reveal_at, admin_can_view_pair, max_gifts_per_giver, created_at FROM groups`

func scanGroup(row *sql.Row) (Group, error) {
	var g Group
	var budget sql.NullInt64
	var eventDate, revealAt sql.NullString
	var canView int
	var created string
	if err := row.Scan(&g.ID, &g.Slug, &g.Name, &budget, &g.BudgetVersion, &g.GiftInstructions,
		&eventDate, &revealAt, &canView, &g.MaxGiftsPerGiver, &created); err != nil {
		return g, err
	}
	if budget.Valid {
		v := budget.Int64
		g.BudgetCents = &v
	}
	g.EventDate = eventDate.String
	if revealAt.Valid {
		t, err := time.Parse(time.RFC3339, revealAt.String)
		if err != nil {
			return g, err
		}
		g.RevealAt = &t
	}
	g.AdminCanViewPair = canView == 1
	t, err := time.Parse(time.RFC3339Nano, created)
	if err != nil {
		return g, err
	}
	g.CreatedAt = t
	return g, nil
}

func (s *Service) one(ctx context.Context, where string, arg any) (*Group, error) {
	g, err := scanGroup(s.db.QueryRowContext(ctx, selectGroup+" WHERE "+where, arg))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, errs.NotFound(msgGroupNotFound)
	}
	if err != nil {
		return nil, errs.Internal(err)
	}
	return &g, nil
}

// ByID lit un groupe par identifiant (usage interne, après authentification).
func (s *Service) ByID(ctx context.Context, id int64) (*Group, error) {
	return s.one(ctx, "id = ?", id)
}

// BySlug lit un groupe par slug (usage interne, après authentification).
func (s *Service) BySlug(ctx context.Context, slug string) (*Group, error) {
	return s.one(ctx, "slug = ?", slug)
}

// PublicPage renvoie les données de la page publique /<slug> : le nom du
// groupe seulement. Un slug inconnu ou invalide renvoie la même erreur
// « introuvable ».
func (s *Service) PublicPage(ctx context.Context, slug string) (PublicView, error) {
	var v PublicView
	err := s.db.QueryRowContext(ctx, `SELECT slug, name FROM groups WHERE slug = ?`, slug).Scan(&v.Slug, &v.Name)
	if errors.Is(err, sql.ErrNoRows) {
		return PublicView{}, errs.NotFound(msgGroupNotFound)
	}
	if err != nil {
		return PublicView{}, errs.Internal(err)
	}
	return v, nil
}
