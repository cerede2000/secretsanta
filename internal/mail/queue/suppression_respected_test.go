package queue_test

import (
	"testing"

	"secretsanta/internal/mail/queue"
)

func TestSuppressedAddressIsNotTried(t *testing.T) {
	e := newEnv(t, nil)
	if err := e.Supp.Add(t.Context(), 1, "Lea@Example.net", "5.1.1"); err != nil {
		t.Fatal(err)
	}
	id := mustEnqueue(t, e, item("lea@example.net", "Léa", 1))
	tick(t, e)
	st := status(t, e, id)
	if st.State != queue.StateFailed || st.LastError != queue.ReasonSuppressed || st.Attempts != 1 {
		t.Fatalf("échec « adresse refusée » attendu : %+v", st)
	}
	if e.Tr.count() != 0 {
		t.Fatal("aucune tentative d'envoi attendue")
	}
	// Adresse corrigée : envoyée normalement.
	id2 := mustEnqueue(t, e, item("lea.corrigee@example.net", "Léa", 1))
	tick(t, e)
	if st := status(t, e, id2); st.State != queue.StateSent || e.Tr.count() != 1 {
		t.Fatalf("envoi attendu pour l'adresse corrigée : %+v", st)
	}
	// Réactivation de l'ancienne adresse.
	_ = e.Supp.Remove(t.Context(), 1, "lea@example.net")
	id3 := mustEnqueue(t, e, item("lea@example.net", "Léa", 1))
	tick(t, e)
	if st := status(t, e, id3); st.State != queue.StateSent {
		t.Fatalf("adresse réactivée : %+v", st)
	}
}
