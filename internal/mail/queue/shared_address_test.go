package queue_test

import (
	"strings"
	"testing"

	"secretsanta/internal/mail/queue"
	"secretsanta/internal/shared/ports"
)

func TestSharedAddressGetsTwoDistinctMails(t *testing.T) {
	e := newEnv(t, nil)
	mustEnqueue(t, e, item("famille@example.net", "Léa", 1))
	mustEnqueue(t, e, item("famille@example.net", "Hugo", 2))
	if tick(t, e) != 2 {
		t.Fatal("deux envois attendus")
	}
	if len(e.Tr.Sent) != 2 {
		t.Fatalf("%d messages", len(e.Tr.Sent))
	}
	a, b := string(e.Tr.Sent[0].Raw), string(e.Tr.Sent[1].Raw)
	for _, c := range []struct{ raw, name, own, other string }{{a, "Léa", "p1", "p2"}, {b, "Hugo", "p2", "p1"}} {
		if !strings.Contains(c.raw, "Bonjour") {
			t.Fatal("salutation absente")
		}
		if !strings.Contains(c.raw, "acces/"+c.own) {
			t.Fatalf("lien de %s absent", c.name)
		}
		if strings.Contains(c.raw, "acces/"+c.other) {
			t.Fatalf("le lien de l'autre participant ne doit pas figurer dans le message de %s", c.name)
		}
		if !strings.Contains(c.raw, "To: ") {
			t.Fatal("en-tête To absent")
		}
	}
	if a == b {
		t.Fatal("messages identiques")
	}
}

func TestPortSendQueuesOneMailPerCall(t *testing.T) {
	e := newEnv(t, nil)
	s := queue.Sender{S: e.Svc}
	var _ ports.EmailSender = s
	for i := 0; i < 2; i++ {
		err := s.Send(t.Context(), ports.EmailMessage{
			To: "famille@example.net", Subject: "Ton tirage", Type: "tirage_pret", Essential: true,
			Body: "Bonjour Léa,\n\nVoir https://noel.example.org/famille/tirage\n",
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	if tick(t, e) != 2 {
		t.Fatal("deux envois attendus")
	}
}
