package queue_test

import (
	"errors"
	"testing"
	"time"

	"secretsanta/internal/mail/queue"
)

func TestStatusReportsSentAndFailed(t *testing.T) {
	e := newEnv(t, func(o *queue.Options) { o.MaxAttempts = 1 })
	e.Tr.Fn = func(_ int, env queue.Envelope) error {
		if env.To == "ko@example.net" {
			return errors.New("boîte pleine")
		}
		return nil
	}
	ok := mustEnqueue(t, e, item("ok@example.net", "Ana", 1))
	ko := mustEnqueue(t, e, item("ko@example.net", "Bob", 2))
	st := status(t, e, ok)
	if st.State != queue.StatePending || st.To != "ok@example.net" || st.EnqueuedAt != t0 {
		t.Fatalf("statut initial : %+v", st)
	}
	e.Clk.Advance(time.Minute)
	tick(t, e)
	st = status(t, e, ok)
	if st.State != queue.StateSent || st.Attempts != 1 || !st.SentAt.Equal(e.Clk.Now()) {
		t.Fatalf("envoyé attendu : %+v", st)
	}
	st = status(t, e, ko)
	if st.State != queue.StateFailed || st.Attempts != 1 || st.LastError == "" {
		t.Fatalf("échec attendu : %+v", st)
	}
	list, err := e.Svc.Recent(t.Context(), 10)
	if err != nil || len(list) != 2 || list[0].ID != ko {
		t.Fatalf("liste récente : %+v %v", list, err)
	}
	if _, err := e.Svc.Status(t.Context(), 9999); err == nil {
		t.Fatal("courriel inconnu : erreur attendue")
	}
}

func TestSendTestReturnsStatus(t *testing.T) {
	e := newEnv(t, nil)
	st, err := e.Svc.SendTest(t.Context(), item("admin@example.net", "Admin", 0))
	if err != nil {
		t.Fatal(err)
	}
	if st.State != queue.StateSent || st.To != "admin@example.net" || st.Attempts != 1 || e.Tr.count() != 1 {
		t.Fatalf("test d'envoi : %+v", st)
	}
	e.Tr.Fn = func(int, queue.Envelope) error { return errors.New("hors service") }
	st, err = e.Svc.SendTest(t.Context(), item("admin@example.net", "Admin", 0))
	if err != nil || st.State != queue.StatePending || st.LastError == "" {
		t.Fatalf("échec temporaire attendu avec erreur : %+v %v", st, err)
	}
}

func TestNotConfiguredFailsInsteadOfCrashing(t *testing.T) {
	e := newEnv(t, func(o *queue.Options) { o.Transport = nil })
	id := mustEnqueue(t, e, item("a@example.net", "Ana", 1))
	tick(t, e)
	st := status(t, e, id)
	if st.State != queue.StateFailed || st.LastError != queue.ReasonNotConfigured {
		t.Fatalf("échec « courrier non configuré » attendu : %+v", st)
	}
}
