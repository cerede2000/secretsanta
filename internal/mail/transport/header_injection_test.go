package transport

import (
	"context"
	"strings"
	"testing"
	"time"

	"secretsanta/internal/mail/queue"
	"secretsanta/internal/mailmsg"
	"secretsanta/internal/shared/clock"
)

// F6.AC3 : aucun retour à la ligne injecté dans les noms, sujets ou adresses
// ne passe ; le transport refuse aussi un message déjà corrompu.
func TestInjectionNeverReachesTheServer(t *testing.T) {
	s := newFakeServer(t, "starttls")
	tr, _ := New(baseOpts(s, "starttls"))
	ctx := context.Background()

	// 1. Enveloppe : adresses.
	for _, bad := range []string{"a@b.fr\r\nRCPT TO:<x@y.fr>", "a@b.fr\n", "a b@c.fr", "<a@b.fr>", ""} {
		e := env()
		e.To = bad
		if err := tr.Send(ctx, e); err == nil {
			t.Errorf("destinataire %q accepté", bad)
		}
		if bad == "" {
			continue // expéditeur vide : repli sur MAIL_FROM
		}
		e = env()
		e.From = bad
		e.Raw = rawMsg("x")
		o := baseOpts(s, "starttls")
		o.Mail.EnvelopeFrom = ""
		tr2, _ := New(o)
		if err := tr2.Send(ctx, e); err == nil {
			t.Errorf("expéditeur %q accepté", bad)
		}
	}

	// 2. En-têtes déjà corrompus dans Raw.
	for _, subj := range []string{"ok\nBcc: v@x.fr", "ok\rBcc: v@x.fr"} {
		e := env()
		e.Raw = rawMsg(subj)
		if err := tr.Send(ctx, e); err == nil {
			t.Errorf("sujet %q accepté", subj)
		}
	}

	// 3. Via le constructeur réel : refusé en amont, et ce qu'il produit passe.
	b, err := mailmsg.New(mailmsg.Options{
		PublicURL: "https://noel.example.org", From: "noel@example.org", FromName: "Père Noël",
		Clock: clock.NewFake(time.Date(2026, 12, 1, 9, 0, 0, 0, time.UTC)),
	})
	if err != nil {
		t.Fatal(err)
	}
	link := "https://noel.example.org/x"
	msg := mailmsg.Message{
		To: "lea@example.net", ToName: "Léa", Subject: "Le tirage est fait", Essential: true,
		Text: "Bonjour Léa,\n\n" + link + "\n", HTML: `<p>Bonjour Léa,</p><p><a href="` + link + `">` + link + `</a></p>`,
	}
	bad := msg
	bad.ToName = "Léa\r\nBcc: v@x.fr"
	if _, err := b.Build(bad); err == nil {
		t.Error("nom piégé accepté par le constructeur")
	}
	built, err := b.Build(msg)
	if err != nil {
		t.Fatal(err)
	}
	if err := tr.Send(ctx, queue.Envelope{From: built.EnvelopeFrom, To: built.To, Raw: built.Raw}); err != nil {
		t.Fatalf("message légitime refusé : %v", err)
	}
	if len(s.data) != 1 || strings.Contains(string(s.data[0]), "\r\nBcc:") {
		t.Fatalf("messages reçus : %d", len(s.data))
	}
	if len(s.rcptTo) != 1 {
		t.Fatalf("destinataires : %v", s.rcptTo)
	}
}
