// Package queue est la file d'envoi persistée des courriels : reprise,
// verrou anti-doublon, délais croissants, regroupement par clé, débit limité
// et une seule connexion SMTP à la fois.
package queue

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"time"

	"secretsanta/internal/mailmsg"
	"secretsanta/internal/shared/clock"
	"secretsanta/internal/shared/errs"
	"secretsanta/internal/shared/ports"
)

// Raisons d'échec lisibles, enregistrées comme dernière erreur.
const (
	ReasonNotConfigured = "courrier non configuré"
	ReasonSuppressed    = "adresse refusée"
	ReasonInvalid       = "message invalide"
	ReasonGaveUp        = "nombre maximal de tentatives atteint"
)

// Valeurs par défaut.
const (
	DefaultMaxAttempts  = 5
	DefaultCoalesceWait = 10 * time.Minute
	DefaultCoalesceMax  = 60 * time.Minute
	DefaultLease        = 10 * time.Minute
	DefaultBackoffBase  = time.Minute
)

// Envelope est un message prêt à partir sur une connexion SMTP.
type Envelope struct {
	From string
	To   string
	Raw  []byte
}

// Transport envoie une enveloppe sur une connexion SMTP. Une erreur qui porte un
// code SMTP (net/textproto.Error ou méthode SMTPCode() int) est classée : 4xx
// temporaire, 5xx définitive ; toute autre erreur est temporaire.
type Transport interface {
	Send(ctx context.Context, env Envelope) error
}

// Builder fabrique le message final (mailmsg.Builder).
type Builder interface {
	Build(m mailmsg.Message) (*mailmsg.Built, error)
}

// Signer signe le message fabriqué (maildkim.Signer).
type Signer interface {
	Sign(msg []byte) ([]byte, error)
}

// Options configure le service.
type Options struct {
	DB      *sql.DB
	Clock   clock.Clock
	Builder Builder
	// Transport est nil quand le courrier n'est pas configuré : les messages
	// passent alors en échec « courrier non configuré ».
	Transport Transport
	Signer    Signer
	// Suppression est facultatif.
	Suppression ports.SuppressionList
	// Delay sépare deux envois ; MaxPerMinute limite le débit (0 = sans limite).
	Delay        time.Duration
	MaxPerMinute int
	// MaxAttempts plafonne les tentatives (DefaultMaxAttempts si 0).
	MaxAttempts int
	// BackoffBase est le premier délai de reprise, doublé à chaque échec.
	BackoffBase  time.Duration
	CoalesceWait time.Duration
	CoalesceMax  time.Duration
	// Lease est la durée après laquelle un verrou abandonné est repris.
	Lease  time.Duration
	Logger *slog.Logger
}

// Service est la file d'envoi.
type Service struct {
	o Options

	// mu garantit une seule connexion SMTP à la fois et protège le débit.
	mu     sync.Mutex
	recent []time.Time
	last   time.Time
}

// New crée le service.
func New(o Options) (*Service, error) {
	if o.DB == nil || o.Builder == nil {
		return nil, errors.New("queue : base et constructeur de messages obligatoires")
	}
	if o.Clock == nil {
		o.Clock = clock.Real{}
	}
	if o.MaxAttempts <= 0 {
		o.MaxAttempts = DefaultMaxAttempts
	}
	if o.BackoffBase <= 0 {
		o.BackoffBase = DefaultBackoffBase
	}
	if o.CoalesceWait <= 0 {
		o.CoalesceWait = DefaultCoalesceWait
	}
	if o.CoalesceMax <= 0 {
		o.CoalesceMax = DefaultCoalesceMax
	}
	if o.Lease <= 0 {
		o.Lease = DefaultLease
	}
	if o.Logger == nil {
		o.Logger = slog.Default()
	}
	return &Service{o: o}, nil
}

// Item est un courriel à mettre en file. Un item par destinataire : deux
// participants qui partagent une adresse donnent deux items distincts.
type Item struct {
	GroupID       int64
	ParticipantID int64
	To            string
	ToName        string
	// Type est la nature du courriel (invitation, relances, ...).
	Type      string
	Essential bool
	Subject   string
	Text      string
	HTML      string
	// UnsubscribeURL est le lien signé d'un courriel non essentiel.
	UnsubscribeURL string
	// CoalesceKey remplace le message en attente de même clé et repousse son envoi.
	CoalesceKey string
}

func nanos(t time.Time) int64 { return t.UnixNano() }

func newToken() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// EnqueueMail met un courriel en file et renvoie son identifiant.
func (s *Service) EnqueueMail(ctx context.Context, it Item) (int64, error) {
	it.To = strings.TrimSpace(it.To)
	if it.To == "" {
		return 0, errs.Validation("L'adresse du destinataire est obligatoire.")
	}
	if strings.TrimSpace(it.Type) == "" {
		return 0, errs.Validation("Le type de courriel est obligatoire.")
	}
	now := s.o.Clock.Now()
	if it.CoalesceKey == "" {
		id, err := s.insert(ctx, it, now, now)
		if err != nil {
			return 0, errs.Internal(err)
		}
		return id, nil
	}
	for i := 0; i < 3; i++ {
		res, err := s.o.DB.ExecContext(ctx, `UPDATE mail_queue SET
			group_id=?, participant_id=?, recipient=?, recipient_name=?, mail_type=?, essential=?,
			subject=?, body_text=?, body_html=?, unsubscribe_url=?, updated_at=?,
			send_after=MIN(?, created_at+?)
			WHERE coalesce_key=? AND status='pending'`,
			it.GroupID, it.ParticipantID, it.To, it.ToName, it.Type, boolInt(it.Essential),
			it.Subject, it.Text, it.HTML, it.UnsubscribeURL, nanos(now),
			nanos(now.Add(s.o.CoalesceWait)), int64(s.o.CoalesceMax), it.CoalesceKey)
		if err != nil {
			return 0, errs.Internal(err)
		}
		if n, _ := res.RowsAffected(); n > 0 {
			var id int64
			err := s.o.DB.QueryRowContext(ctx, `SELECT id FROM mail_queue WHERE coalesce_key=? AND status='pending'`, it.CoalesceKey).Scan(&id)
			if err != nil {
				return 0, errs.Internal(err)
			}
			return id, nil
		}
		id, err := s.insert(ctx, it, now, now.Add(s.o.CoalesceWait))
		if err == nil {
			return id, nil
		}
		if !strings.Contains(err.Error(), "UNIQUE") {
			return 0, err
		}
	}
	return 0, errs.Internal(errors.New("regroupement concurrent"))
}

func (s *Service) insert(ctx context.Context, it Item, now, sendAfter time.Time) (int64, error) {
	res, err := s.o.DB.ExecContext(ctx, `INSERT INTO mail_queue
		(group_id, participant_id, recipient, recipient_name, mail_type, essential, subject, body_text, body_html,
		 unsubscribe_url, coalesce_key, status, created_at, updated_at, send_after)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,'pending',?,?,?)`,
		it.GroupID, it.ParticipantID, it.To, it.ToName, it.Type, boolInt(it.Essential), it.Subject, it.Text, it.HTML,
		it.UnsubscribeURL, it.CoalesceKey, nanos(now), nanos(now), nanos(sendAfter))
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
