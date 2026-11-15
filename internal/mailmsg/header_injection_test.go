package mailmsg

import (
	"net/mail"
	"slices"
	"strings"
	"testing"
)

// F6b.AC4 : tout retour à la ligne dans un nom, un objet ou une adresse est
// refusé ; aucun en-tête supplémentaire ne peut être injecté.

var injections = []string{
	"\r\nBcc: victime@example.com",
	"\nBcc: victime@example.com",
	"\rBcc: victime@example.com",
	"\r\n\r\n<p>corps injecté</p>",
	" Bcc: victime@example.com",
	"\u0085Bcc: victime@example.com",
	"\x00",
	"\t",
}

func TestInjectionInBuilderOptionsRefused(t *testing.T) {
	fields := map[string]func(*Options, string){
		"FromName":     func(o *Options, p string) { o.FromName = "Père Noël" + p },
		"From":         func(o *Options, p string) { o.From = testFrom + p },
		"EnvelopeFrom": func(o *Options, p string) { o.EnvelopeFrom = testFrom + p },
		"ReplyTo":      func(o *Options, p string) { o.ReplyTo = "orga@example.org" + p },
	}
	for name, set := range fields {
		for _, p := range injections {
			o := testOptions()
			set(&o, p)
			_, err := New(o)
			if err == nil {
				t.Errorf("%s avec %q accepté", name, p)
				continue
			}
			assertKind(t, err, ErrHeaderInjection)
		}
	}
}

func TestInjectionInMessageRefused(t *testing.T) {
	fields := map[string]func(*Message, string){
		"To":                func(m *Message, p string) { m.To = testTo + p },
		"ToName":            func(m *Message, p string) { m.ToName = "Léa" + p },
		"Subject":           func(m *Message, p string) { m.Subject = "Le tirage est fait" + p },
		"UnsubscribeURL":    func(m *Message, p string) { m.UnsubscribeURL = "https://noel.example.org/stop" + p },
		"UnsubscribeMailto": func(m *Message, p string) { m.UnsubscribeMailto = "mailto:stop@example.org" + p },
	}
	b := newBuilder(t, nil)
	for name, set := range fields {
		for _, p := range injections {
			m := validMessage()
			m.Essential = false
			m.UnsubscribeURL = "https://noel.example.org/desinscription"
			set(&m, p)
			_, err := b.Build(m)
			if err == nil {
				t.Errorf("%s avec %q accepté", name, p)
				continue
			}
			assertKind(t, err, ErrHeaderInjection)
		}
	}
}

func TestInjectionMultipleOrDecoratedAddressesRefused(t *testing.T) {
	b := newBuilder(t, nil)
	for _, to := range []string{
		"lea@example.net, victime@example.com",
		"lea@example.net;victime@example.com",
		"Léa <lea@example.net>",
		"<lea@example.net>",
		"lea@example.net (commentaire)",
		"\"lea\"@example.net",
		"lea",
		"lea@localhost",
		"",
	} {
		m := validMessage()
		m.To = to
		_, err := b.Build(m)
		assertKind(t, err, ErrAddress)
	}
}

func TestInjectionHostileNameStaysInsideItsHeader(t *testing.T) {
	m := validMessage()
	m.ToName = `Léa "la reine" <evil@example.com>, Bcc: victime@example.com; Subject: X`
	m.Subject = `Objet ; Bcc: victime@example.com, Cc: autre@example.com`
	out := build(t, newBuilder(t, func(o *Options) {
		o.FromName = `Noël <evil@example.com>, Bcc: victime@example.com`
	}), m)
	msg := parse(t, out.Raw)

	var keys []string
	for k := range msg.Header {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	want := []string{"Auto-Submitted", "Content-Language", "Content-Type", "Date", "From", "Message-Id", "Mime-Version", "Subject", "To", "X-Auto-Response-Suppress"}
	if !slices.Equal(keys, want) {
		t.Fatalf("en-têtes %v, attendu %v", keys, want)
	}
	to, err := mail.ParseAddressList(msg.Header.Get("To"))
	if err != nil || len(to) != 1 || to[0].Address != testTo || to[0].Name != m.ToName {
		t.Errorf("To altéré : %v (%v)", to, err)
	}
	from, err := mail.ParseAddressList(msg.Header.Get("From"))
	if err != nil || len(from) != 1 || from[0].Address != testFrom {
		t.Errorf("From altéré : %v (%v)", from, err)
	}
	for _, l := range headerLines(out.Raw) {
		if strings.HasPrefix(strings.ToLower(l), "bcc:") || strings.HasPrefix(strings.ToLower(l), "cc:") {
			t.Errorf("en-tête injecté : %q", l)
		}
	}
}

func TestInjectionBodyCannotAddHeaders(t *testing.T) {
	m := validMessage()
	m.Text = "Bcc: victime@example.com\r\n\r\n" + m.Text
	m.HTML = "<p>Bcc: victime@example.com</p>\n" + m.HTML
	out := build(t, newBuilder(t, nil), m)
	msg := parse(t, out.Raw)
	if _, ok := msg.Header["Bcc"]; ok {
		t.Errorf("le corps a injecté un en-tête Bcc")
	}
}
