package queue

import (
	"context"
	"database/sql"
	"errors"
	"net/textproto"
	"regexp"
	"strings"
	"time"
	"unicode"

	"secretsanta/internal/mailmsg"
	"secretsanta/internal/shared/errs"
)

type row struct {
	id            int64
	groupID       int64
	participantID int64
	to, toName    string
	mailType      string
	essential     bool
	subject       string
	text, html    string
	lang          string
	unsubURL      string
	attempts      int
	token         string
}

// Tick envoie les messages dus, un par un, sur une seule connexion, tant que le
// délai entre envois et le débit maximal le permettent. Il renvoie le nombre de
// messages réellement remis au transport.
func (s *Service) Tick(ctx context.Context) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.recoverStale(ctx); err != nil {
		return 0, err
	}
	n := 0
	for {
		if err := ctx.Err(); err != nil {
			return n, err
		}
		r, ok, err := s.claim(ctx, 0)
		if err != nil {
			return n, err
		}
		if !ok {
			return n, nil
		}
		attempted, throttled, err := s.process(ctx, r, false)
		if err != nil {
			return n, err
		}
		if attempted {
			n++
		}
		if throttled {
			return n, nil
		}
	}
}

// Run appelle Tick à intervalle régulier jusqu'à l'annulation du contexte
// (la reprise après redémarrage est automatique : l'état est en base).
func (s *Service) Run(ctx context.Context, every time.Duration) {
	if every <= 0 {
		every = time.Second
	}
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		if _, err := s.Tick(ctx); err != nil && ctx.Err() == nil {
			s.o.Logger.Error("file d'envoi", "err", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

// recoverStale remet en attente les messages dont l'envoyeur a disparu.
func (s *Service) recoverStale(ctx context.Context) error {
	cutoff := s.o.Clock.Now().Add(-s.o.Lease)
	_, err := s.o.DB.ExecContext(ctx, `UPDATE mail_queue SET status='pending', lock_token='', locked_at=NULL
		WHERE status='sending' AND locked_at < ?`, nanos(cutoff))
	if err != nil {
		return errs.Internal(err)
	}
	return nil
}

// claim réserve atomiquement le prochain message dû (ou le message id si id > 0).
func (s *Service) claim(ctx context.Context, id int64) (row, bool, error) {
	now := s.o.Clock.Now()
	for i := 0; i < 5; i++ {
		var cand int64
		var err error
		if id > 0 {
			cand = id
		} else {
			err = s.o.DB.QueryRowContext(ctx, `SELECT id FROM mail_queue WHERE status='pending' AND send_after<=?
				ORDER BY send_after, id LIMIT 1`, nanos(now)).Scan(&cand)
			if errors.Is(err, sql.ErrNoRows) {
				return row{}, false, nil
			}
			if err != nil {
				return row{}, false, errs.Internal(err)
			}
		}
		tok := newToken()
		res, err := s.o.DB.ExecContext(ctx, `UPDATE mail_queue SET status='sending', lock_token=?, locked_at=?, attempts=attempts+1
			WHERE id=? AND status='pending'`, tok, nanos(now), cand)
		if err != nil {
			return row{}, false, errs.Internal(err)
		}
		if n, _ := res.RowsAffected(); n == 0 {
			if id > 0 {
				return row{}, false, nil
			}
			continue // pris par un autre envoyeur
		}
		r := row{token: tok}
		var ess int
		err = s.o.DB.QueryRowContext(ctx, `SELECT id, group_id, participant_id, recipient, recipient_name, mail_type, essential,
			subject, body_text, body_html, lang, unsubscribe_url, attempts FROM mail_queue WHERE id=?`, cand).
			Scan(&r.id, &r.groupID, &r.participantID, &r.to, &r.toName, &r.mailType, &ess, &r.subject, &r.text, &r.html, &r.lang, &r.unsubURL, &r.attempts)
		if err != nil {
			return row{}, false, errs.Internal(err)
		}
		r.essential = ess == 1
		return r, true, nil
	}
	return row{}, false, nil
}

// process traite un message réservé. attempted vaut vrai quand le transport a
// été appelé ; throttled quand le débit impose d'attendre (le message est rendu).
func (s *Service) process(ctx context.Context, r row, force bool) (attempted, throttled bool, err error) {
	now := s.o.Clock.Now()
	if s.o.Transport == nil {
		return false, false, s.fail(ctx, r, ReasonNotConfigured)
	}
	if s.o.Suppression != nil {
		sup, serr := s.o.Suppression.IsSuppressed(ctx, r.groupID, r.to)
		if serr != nil {
			return false, false, s.retryLater(ctx, r, serr)
		}
		if sup {
			return false, false, s.fail(ctx, r, ReasonSuppressed)
		}
	}
	built, berr := s.o.Builder.Build(mailmsg.Message{
		To: r.to, ToName: r.toName, Subject: r.subject, Text: r.text, HTML: r.html, Lang: r.lang,
		Essential: r.essential, UnsubscribeURL: r.unsubURL,
	})
	if berr != nil {
		return false, false, s.fail(ctx, r, ReasonInvalid+" : "+Clean(berr))
	}
	raw := built.Raw
	if s.o.Signer != nil {
		if raw, berr = s.o.Signer.Sign(raw); berr != nil {
			return false, false, s.fail(ctx, r, ReasonInvalid+" : "+Clean(berr))
		}
	}
	if !force && !s.allowed(now) {
		return false, true, s.release(ctx, r)
	}
	s.note(now)
	serr := s.o.Transport.Send(ctx, Envelope{From: built.EnvelopeFrom, To: built.To, Raw: raw})
	if serr == nil {
		return true, false, s.markSent(ctx, r)
	}
	if code := smtpCode(serr); code >= 500 && code < 600 {
		if s.o.Suppression != nil {
			if aerr := s.o.Suppression.Add(ctx, r.groupID, r.to, Clean(serr)); aerr != nil {
				s.o.Logger.Error("liste de suppression", "err", aerr)
			}
		}
		return true, false, s.fail(ctx, r, Clean(serr))
	}
	return true, false, s.retryLater(ctx, r, serr)
}

// allowed applique le délai entre envois et la limite par minute.
func (s *Service) allowed(now time.Time) bool {
	if s.o.Delay > 0 && !s.last.IsZero() && now.Before(s.last.Add(s.o.Delay)) {
		return false
	}
	if s.o.MaxPerMinute > 0 {
		keep := s.recent[:0]
		for _, t := range s.recent {
			if now.Sub(t) < time.Minute {
				keep = append(keep, t)
			}
		}
		s.recent = keep
		if len(s.recent) >= s.o.MaxPerMinute {
			return false
		}
	}
	return true
}

func (s *Service) note(now time.Time) {
	s.last = now
	s.recent = append(s.recent, now)
}

func (s *Service) release(ctx context.Context, r row) error {
	_, err := s.o.DB.ExecContext(ctx, `UPDATE mail_queue SET status='pending', lock_token='', locked_at=NULL, attempts=attempts-1
		WHERE id=? AND lock_token=?`, r.id, r.token)
	return wrap(err)
}

func (s *Service) markSent(ctx context.Context, r row) error {
	now := nanos(s.o.Clock.Now())
	_, err := s.o.DB.ExecContext(ctx, `UPDATE mail_queue SET status='sent', sent_at=?, updated_at=?, last_error='', lock_token='', locked_at=NULL
		WHERE id=? AND lock_token=?`, now, now, r.id, r.token)
	return wrap(err)
}

func (s *Service) fail(ctx context.Context, r row, reason string) error {
	now := nanos(s.o.Clock.Now())
	_, err := s.o.DB.ExecContext(ctx, `UPDATE mail_queue SET status='failed', failed_at=?, updated_at=?, last_error=?, lock_token='', locked_at=NULL
		WHERE id=? AND lock_token=?`, now, now, truncate(reason), r.id, r.token)
	return wrap(err)
}

// retryLater replanifie avec un délai doublé à chaque échec, puis abandonne au plafond.
func (s *Service) retryLater(ctx context.Context, r row, cause error) error {
	if r.attempts >= s.o.MaxAttempts {
		return s.fail(ctx, r, ReasonGaveUp+" : "+Clean(cause))
	}
	wait := s.o.BackoffBase
	for i := 1; i < r.attempts && wait < 24*time.Hour; i++ {
		wait *= 2
	}
	now := s.o.Clock.Now()
	_, err := s.o.DB.ExecContext(ctx, `UPDATE mail_queue SET status='pending', send_after=?, updated_at=?, last_error=?, lock_token='', locked_at=NULL
		WHERE id=? AND lock_token=?`, nanos(now.Add(wait)), nanos(now), truncate(Clean(cause)), r.id, r.token)
	return wrap(err)
}

func wrap(err error) error {
	if err != nil {
		return errs.Internal(err)
	}
	return nil
}

// smtpCode extrait le code SMTP d'une erreur (0 s'il n'y en a pas).
func smtpCode(err error) int {
	var te *textproto.Error
	if errors.As(err, &te) {
		return te.Code
	}
	var c interface{ SMTPCode() int }
	if errors.As(err, &c) {
		return c.SMTPCode()
	}
	return 0
}

var credentials = regexp.MustCompile(`(?i)([a-z][a-z0-9+.-]*://)[^/\s@]*@`)

const maxErrorRunes = 300

// Clean nettoie une erreur pour l'affichage : caractères de contrôle retirés,
// espaces normalisés, identifiants d'URL masqués, longueur plafonnée.
func Clean(err error) string {
	if err == nil {
		return ""
	}
	return truncate(cleanString(err.Error()))
}

func cleanString(s string) string {
	s = credentials.ReplaceAllString(s, "${1}***@")
	s = strings.Map(func(r rune) rune {
		if unicode.IsSpace(r) {
			return ' '
		}
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, s)
	return strings.Join(strings.Fields(s), " ")
}

func truncate(s string) string {
	s = cleanString(s)
	if r := []rune(s); len(r) > maxErrorRunes {
		return string(r[:maxErrorRunes]) + "…"
	}
	return s
}
