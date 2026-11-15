package websec

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"time"

	"secretsanta/internal/shared/clock"
	"secretsanta/internal/shared/random"
)

// Kind distingue la session d'administrateur de groupe et celle de participant.
type Kind string

const (
	KindAdmin       Kind = "admin"
	KindParticipant Kind = "participant"
)

// Durées par défaut (absolues, sans prolongation glissante).
const (
	DefaultAdminTTL       = 12 * time.Hour
	DefaultParticipantTTL = 7 * 24 * time.Hour
)

const (
	adminCookieBase       = "ss_admin"
	participantCookieBase = "ss_participant"
)

// ErrNoSession signale une session absente, inconnue, expirée ou d'un autre groupe.
var ErrNoSession = errors.New("websec: session absente ou invalide")

// Identity est l'identité retenue, confiée par un gestionnaire de connexion
// qui a déjà vérifié le secret. Les identifiants sont opaques pour websec.
type Identity struct {
	Kind    Kind
	GroupID string
	// Subject identifie la personne (participant) ; facultatif pour l'administrateur.
	Subject string
}

// Session est une session valide.
type Session struct {
	Identity
	CreatedAt time.Time
	ExpiresAt time.Time
}

// SessionOptions configure le service.
type SessionOptions struct {
	AdminTTL       time.Duration // défaut DefaultAdminTTL
	ParticipantTTL time.Duration // défaut DefaultParticipantTTL
}

// Sessions est le service de sessions, stockées en base. Le cookie porte un
// identifiant aléatoire de 128 bits ; seule son empreinte SHA-256 est stockée.
type Sessions struct {
	db     *sql.DB
	clock  clock.Clock
	policy Policy
	ttl    map[Kind]time.Duration
}

// NewSessions crée le service.
func NewSessions(d *sql.DB, clk clock.Clock, p Policy, opts SessionOptions) *Sessions {
	if opts.AdminTTL <= 0 {
		opts.AdminTTL = DefaultAdminTTL
	}
	if opts.ParticipantTTL <= 0 {
		opts.ParticipantTTL = DefaultParticipantTTL
	}
	return &Sessions{
		db: d, clock: clk, policy: p,
		ttl: map[Kind]time.Duration{KindAdmin: opts.AdminTTL, KindParticipant: opts.ParticipantTTL},
	}
}

func cookieBase(k Kind) (string, http.SameSite, error) {
	switch k {
	case KindAdmin:
		return adminCookieBase, http.SameSiteStrictMode, nil
	case KindParticipant:
		// Lax : le participant arrive par un lien de courriel.
		return participantCookieBase, http.SameSiteLaxMode, nil
	}
	return "", 0, fmt.Errorf("websec: type de session inconnu %q", k)
}

// CookieName renvoie le nom du cookie de session du type donné.
func (s *Sessions) CookieName(k Kind) string {
	base, _, _ := cookieBase(k)
	return s.policy.cookieName(base)
}

func hashID(id string) string {
	sum := sha256.Sum256([]byte(id))
	return hex.EncodeToString(sum[:])
}

func validID(v string) bool {
	b, err := base64.RawURLEncoding.DecodeString(v)
	return err == nil && len(b) == random.TokenBytes
}

// Create ouvre une session neuve pour l'identité et pose son cookie. Une
// session du même type déjà présente sur la requête est supprimée
// (pas de fixation de session).
func (s *Sessions) Create(ctx context.Context, w http.ResponseWriter, r *http.Request, id Identity) (*Session, error) {
	base, sameSite, err := cookieBase(id.Kind)
	if err != nil {
		return nil, err
	}
	if id.GroupID == "" {
		return nil, errors.New("websec: groupe de la session manquant")
	}
	if id.Kind == KindParticipant && id.Subject == "" {
		return nil, errors.New("websec: participant de la session manquant")
	}
	if r != nil {
		if ck, err := r.Cookie(s.policy.cookieName(base)); err == nil && validID(ck.Value) {
			if _, err := s.db.ExecContext(ctx, `DELETE FROM websec_sessions WHERE id_hash = ?`, hashID(ck.Value)); err != nil {
				return nil, fmt.Errorf("websec: suppression de l'ancienne session : %w", err)
			}
		}
	}
	raw, err := random.Token()
	if err != nil {
		return nil, fmt.Errorf("websec: aléa : %w", err)
	}
	now := s.clock.Now()
	ttl := s.ttl[id.Kind]
	sess := &Session{Identity: id, CreatedAt: now, ExpiresAt: now.Add(ttl)}
	if _, err := s.db.ExecContext(ctx,
		`INSERT INTO websec_sessions (id_hash, kind, group_id, subject, created_at, expires_at) VALUES (?, ?, ?, ?, ?, ?)`,
		hashID(raw), string(id.Kind), id.GroupID, id.Subject, now.UnixNano(), sess.ExpiresAt.UnixNano()); err != nil {
		return nil, fmt.Errorf("websec: création de session : %w", err)
	}
	ck := s.policy.cookie(base, raw, sameSite, int(ttl/time.Second))
	ck.Expires = sess.ExpiresAt.UTC()
	http.SetCookie(w, ck)
	return sess, nil
}

// Validate renvoie la session du type donné portée par la requête, si elle
// existe, n'est pas expirée et appartient au groupe donné. Sinon ErrNoSession.
// La session d'un groupe n'est jamais valide pour un autre.
func (s *Sessions) Validate(ctx context.Context, r *http.Request, k Kind, groupID string) (*Session, error) {
	sess, err := s.load(ctx, r, k)
	if err != nil {
		return nil, err
	}
	if groupID == "" || sess.GroupID != groupID {
		return nil, ErrNoSession
	}
	return sess, nil
}

func (s *Sessions) load(ctx context.Context, r *http.Request, k Kind) (*Session, error) {
	base, _, err := cookieBase(k)
	if err != nil {
		return nil, err
	}
	ck, err := r.Cookie(s.policy.cookieName(base))
	if err != nil || !validID(ck.Value) {
		return nil, ErrNoSession
	}
	h := hashID(ck.Value)
	var (
		kind, group, subject string
		created, expires     int64
	)
	err = s.db.QueryRowContext(ctx,
		`SELECT kind, group_id, subject, created_at, expires_at FROM websec_sessions WHERE id_hash = ?`, h).
		Scan(&kind, &group, &subject, &created, &expires)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNoSession
	}
	if err != nil {
		return nil, fmt.Errorf("websec: lecture de session : %w", err)
	}
	if Kind(kind) != k {
		return nil, ErrNoSession
	}
	now := s.clock.Now()
	exp := time.Unix(0, expires).UTC()
	if !now.Before(exp) {
		_, _ = s.db.ExecContext(ctx, `DELETE FROM websec_sessions WHERE id_hash = ?`, h)
		return nil, ErrNoSession
	}
	return &Session{
		Identity:  Identity{Kind: k, GroupID: group, Subject: subject},
		CreatedAt: time.Unix(0, created).UTC(),
		ExpiresAt: exp,
	}, nil
}

// Destroy supprime la session du type donné portée par la requête et efface
// son cookie (déconnexion).
func (s *Sessions) Destroy(ctx context.Context, w http.ResponseWriter, r *http.Request, k Kind) error {
	base, sameSite, err := cookieBase(k)
	if err != nil {
		return err
	}
	if ck, err := r.Cookie(s.policy.cookieName(base)); err == nil && validID(ck.Value) {
		if _, err := s.db.ExecContext(ctx, `DELETE FROM websec_sessions WHERE id_hash = ?`, hashID(ck.Value)); err != nil {
			return fmt.Errorf("websec: suppression de session : %w", err)
		}
	}
	http.SetCookie(w, s.policy.cookie(base, "", sameSite, -1))
	return nil
}

// PurgeExpired supprime les sessions expirées (tâche périodique).
func (s *Sessions) PurgeExpired(ctx context.Context) (int64, error) {
	res, err := s.db.ExecContext(ctx, `DELETE FROM websec_sessions WHERE expires_at <= ?`, s.clock.Now().UnixNano())
	if err != nil {
		return 0, fmt.Errorf("websec: purge des sessions : %w", err)
	}
	return res.RowsAffected()
}

type sessionsCtxKey struct{}

type loadedSessions map[Kind]*Session

// Middleware charge les sessions valides (non expirées) de la requête dans
// son contexte. L'accès passe obligatoirement par AdminSession ou
// ParticipantSession, qui exigent le groupe.
func (s *Sessions) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		loaded := loadedSessions{}
		for _, k := range []Kind{KindAdmin, KindParticipant} {
			sess, err := s.load(r.Context(), r, k)
			if err == nil {
				loaded[k] = sess
				continue
			}
			if !errors.Is(err, ErrNoSession) {
				http.Error(w, "Une erreur interne est survenue. Réessaie plus tard.", http.StatusInternalServerError)
				return
			}
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), sessionsCtxKey{}, loaded)))
	})
}

func fromContext(ctx context.Context, k Kind, groupID string) (*Session, bool) {
	loaded, _ := ctx.Value(sessionsCtxKey{}).(loadedSessions)
	sess, ok := loaded[k]
	if !ok || groupID == "" || sess.GroupID != groupID {
		return nil, false
	}
	return sess, true
}

// AdminSession renvoie la session administrateur chargée par Middleware si
// elle appartient au groupe donné.
func AdminSession(ctx context.Context, groupID string) (*Session, bool) {
	return fromContext(ctx, KindAdmin, groupID)
}

// ParticipantSession renvoie la session participant chargée par Middleware si
// elle appartient au groupe donné.
func ParticipantSession(ctx context.Context, groupID string) (*Session, bool) {
	return fromContext(ctx, KindParticipant, groupID)
}
