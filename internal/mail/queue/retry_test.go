package queue_test

import (
	"errors"
	"strings"
	"testing"
	"time"

	"secretsanta/internal/mail/queue"
)

func TestRetryBackoffThenFail(t *testing.T) {
	e := newEnv(t, func(o *queue.Options) { o.MaxAttempts = 4; o.BackoffBase = time.Minute })
	e.Tr.Fn = func(int, queue.Envelope) error {
		return errors.New("connexion refusée\x00\r\n smtp://user:secret@mail.example.org")
	}
	id := mustEnqueue(t, e, item("lea@example.net", "Léa", 1))

	var waits []time.Duration
	for i := 1; i <= 4; i++ {
		before := e.Clk.Now()
		if n := tick(t, e); n != 1 {
			t.Fatalf("tentative %d : %d envoi(s)", i, n)
		}
		st := status(t, e, id)
		if st.Attempts != i {
			t.Fatalf("tentatives %d, attendu %d", st.Attempts, i)
		}
		if i < 4 {
			if st.State != queue.StatePending {
				t.Fatalf("en attente attendu, obtenu %s", st.State)
			}
			w := st.NextAttemptAt.Sub(before)
			waits = append(waits, w)
			if tick(t, e) != 0 {
				t.Fatal("aucune reprise avant l'échéance")
			}
			e.Clk.Advance(w)
		}
	}
	for i := 1; i < len(waits); i++ {
		if waits[i] <= waits[i-1] {
			t.Fatalf("délais non croissants : %v", waits)
		}
	}
	st := status(t, e, id)
	if st.State != queue.StateFailed {
		t.Fatalf("échec attendu, obtenu %s", st.State)
	}
	for _, bad := range []string{"secret", "\x00", "\r", "\n"} {
		if strings.Contains(st.LastError, bad) {
			t.Fatalf("erreur non nettoyée : %q", st.LastError)
		}
	}
	if st.LastError == "" {
		t.Fatal("dernière erreur attendue")
	}
	e.Clk.Advance(48 * time.Hour)
	if tick(t, e) != 0 || e.Tr.count() != 4 {
		t.Fatalf("aucune reprise après l'échec, %d envois", e.Tr.count())
	}
}

func TestSuccessAfterRetry(t *testing.T) {
	e := newEnv(t, nil)
	e.Tr.Fn = func(call int, _ queue.Envelope) error {
		if call == 1 {
			return errors.New("temporaire")
		}
		return nil
	}
	id := mustEnqueue(t, e, item("lea@example.net", "Léa", 1))
	tick(t, e)
	e.Clk.Advance(time.Hour)
	tick(t, e)
	st := status(t, e, id)
	if st.State != queue.StateSent || st.Attempts != 2 || st.SentAt.IsZero() || st.LastError != "" {
		t.Fatalf("envoyé après reprise attendu : %+v", st)
	}
}
