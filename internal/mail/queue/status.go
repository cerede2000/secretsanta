package queue

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"secretsanta/internal/shared/errs"
)

// State est l'état public d'un courriel.
type State string

// États publics (le verrou interne « sending » est vu comme « pending »).
const (
	StatePending State = "pending"
	StateSent    State = "sent"
	StateFailed  State = "failed"
)

// Status est le statut consultable d'un courriel.
type Status struct {
	ID            int64
	To            string
	Type          string
	State         State
	Attempts      int
	LastError     string
	EnqueuedAt    time.Time
	UpdatedAt     time.Time
	NextAttemptAt time.Time // en attente seulement
	SentAt        time.Time // zéro tant que non envoyé
}

const statusCols = `id, recipient, mail_type, status, attempts, last_error, created_at, updated_at, send_after, sent_at`

type scanner interface{ Scan(...any) error }

func scanStatus(sc scanner) (Status, error) {
	var st Status
	var state string
	var created, updated, after int64
	var sent sql.NullInt64
	if err := sc.Scan(&st.ID, &st.To, &st.Type, &state, &st.Attempts, &st.LastError, &created, &updated, &after, &sent); err != nil {
		return Status{}, err
	}
	st.State = State(state)
	if st.State == "sending" {
		st.State = StatePending
	}
	st.EnqueuedAt, st.UpdatedAt = time.Unix(0, created).UTC(), time.Unix(0, updated).UTC()
	if st.State == StatePending {
		st.NextAttemptAt = time.Unix(0, after).UTC()
	}
	if sent.Valid {
		st.SentAt = time.Unix(0, sent.Int64).UTC()
	}
	return st, nil
}

// Status renvoie le statut du courriel id.
func (s *Service) Status(ctx context.Context, id int64) (Status, error) {
	st, err := scanStatus(s.o.DB.QueryRowContext(ctx, `SELECT `+statusCols+` FROM mail_queue WHERE id=?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return Status{}, errs.NotFound("Courriel introuvable.")
	}
	if err != nil {
		return Status{}, errs.Internal(err)
	}
	return st, nil
}

// Recent liste les derniers courriels, du plus récent au plus ancien.
func (s *Service) Recent(ctx context.Context, limit int) ([]Status, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	rows, err := s.o.DB.QueryContext(ctx, `SELECT `+statusCols+` FROM mail_queue ORDER BY id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, errs.Internal(err)
	}
	defer rows.Close()
	var out []Status
	for rows.Next() {
		st, err := scanStatus(rows)
		if err != nil {
			return nil, errs.Internal(err)
		}
		out = append(out, st)
	}
	if err := rows.Err(); err != nil {
		return nil, errs.Internal(err)
	}
	return out, nil
}

// SendTest met en file un message de test (essentiel, sans regroupement), tente
// de l'envoyer aussitôt et renvoie son statut. Le contenu est fourni par l'appelant.
func (s *Service) SendTest(ctx context.Context, it Item) (Status, error) {
	it.Essential = true
	it.CoalesceKey = ""
	if it.Type == "" {
		it.Type = "test"
	}
	id, err := s.EnqueueMail(ctx, it)
	if err != nil {
		return Status{}, err
	}
	s.mu.Lock()
	r, ok, err := s.claim(ctx, id)
	if err == nil && ok {
		_, _, err = s.process(ctx, r, true)
	}
	s.mu.Unlock()
	if err != nil {
		return Status{}, err
	}
	return s.Status(ctx, id)
}
