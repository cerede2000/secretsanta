package queue_test

import (
	"net/textproto"
	"testing"
	"time"

	"secretsanta/internal/mail/queue"
)

func TestTemporary4xxIsRetried(t *testing.T) {
	e := newEnv(t, nil)
	e.Tr.Fn = func(call int, _ queue.Envelope) error {
		if call < 3 {
			return &textproto.Error{Code: 451, Msg: "réessayez plus tard"}
		}
		return nil
	}
	id := mustEnqueue(t, e, item("lea@example.net", "Léa", 1))
	var prev time.Duration
	for i := 1; i <= 2; i++ {
		tick(t, e)
		st := status(t, e, id)
		if st.State != queue.StatePending {
			t.Fatalf("4xx : en attente attendu, %s", st.State)
		}
		w := st.NextAttemptAt.Sub(e.Clk.Now())
		if w <= prev {
			t.Fatalf("délais croissants attendus : %v puis %v", prev, w)
		}
		prev = w
		e.Clk.Advance(w)
	}
	tick(t, e)
	if st := status(t, e, id); st.State != queue.StateSent || st.Attempts != 3 {
		t.Fatalf("envoyé à la 3e tentative attendu : %+v", st)
	}
	if ok, _ := e.Supp.IsSuppressed(t.Context(), 1, "lea@example.net"); ok {
		t.Fatal("un 4xx n'inscrit pas l'adresse")
	}
}

func TestPermanent5xxNeverRetriedAndSuppresses(t *testing.T) {
	e := newEnv(t, nil)
	e.Tr.Fn = func(int, queue.Envelope) error {
		return &textproto.Error{Code: 550, Msg: "5.1.1 utilisateur inconnu"}
	}
	id := mustEnqueue(t, e, item("lea@example.net", "Léa", 1))
	tick(t, e)
	st := status(t, e, id)
	if st.State != queue.StateFailed || st.Attempts != 1 || st.LastError == "" {
		t.Fatalf("échec immédiat attendu : %+v", st)
	}
	e.Clk.Advance(72 * time.Hour)
	tick(t, e)
	if e.Tr.count() != 1 {
		t.Fatalf("aucun réessai attendu, %d envois", e.Tr.count())
	}
	if ok, _ := e.Supp.IsSuppressed(t.Context(), 1, "lea@example.net"); !ok {
		t.Fatal("adresse attendue dans la liste de suppression")
	}
	if ok, _ := e.Supp.IsSuppressed(t.Context(), 2, "lea@example.net"); ok {
		t.Fatal("suppression propre au groupe")
	}
}

type codeErr struct{ code int }

func (c codeErr) Error() string { return "erreur" }
func (c codeErr) SMTPCode() int { return c.code }

func TestSMTPCodeMethodIsRecognised(t *testing.T) {
	e := newEnv(t, nil)
	e.Tr.Fn = func(int, queue.Envelope) error { return codeErr{554} }
	id := mustEnqueue(t, e, item("lea@example.net", "Léa", 1))
	tick(t, e)
	if st := status(t, e, id); st.State != queue.StateFailed {
		t.Fatalf("échec attendu : %+v", st)
	}
}
