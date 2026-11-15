package mailmsg

import (
	"net/mail"
	"regexp"
	"strings"
	"testing"
	"time"
)

// F6b.AC2 : en-têtes obligatoires présents et corrects.

func TestHeadersMandatoryPresentOnce(t *testing.T) {
	out := build(t, newBuilder(t, nil), validMessage())
	msg := parse(t, out.Raw)
	want := map[string]string{
		"Mime-Version":             "1.0",
		"Content-Language":         "fr",
		"Auto-Submitted":           "auto-generated",
		"X-Auto-Response-Suppress": "All",
	}
	for k, v := range want {
		got := msg.Header[k]
		if len(got) != 1 || got[0] != v {
			t.Errorf("%s = %v, attendu exactement une fois %q", k, got, v)
		}
	}
	for _, k := range []string{"From", "To", "Subject", "Date", "Message-Id", "Content-Type"} {
		if n := len(msg.Header[k]); n != 1 {
			t.Errorf("%s présent %d fois, attendu 1", k, n)
		}
	}
	if _, ok := msg.Header["Reply-To"]; ok {
		t.Errorf("Reply-To présent sans être configuré")
	}
}

func TestHeadersFromDisplayNameAlignedWithEnvelope(t *testing.T) {
	out := build(t, newBuilder(t, nil), validMessage())
	msg := parse(t, out.Raw)
	from, err := mail.ParseAddress(msg.Header.Get("From"))
	if err != nil {
		t.Fatalf("From illisible : %v", err)
	}
	if from.Name != testFromName || from.Address != testFrom {
		t.Errorf("From = %q <%s>, attendu %q <%s>", from.Name, from.Address, testFromName, testFrom)
	}
	if out.EnvelopeFrom != testFrom {
		t.Errorf("expéditeur d'enveloppe %q, attendu %q par défaut", out.EnvelopeFrom, testFrom)
	}
	if out.To != testTo {
		t.Errorf("destinataire d'enveloppe %q", out.To)
	}

	// Nom absent : nom par défaut, jamais une adresse nue.
	out = build(t, newBuilder(t, func(o *Options) { o.FromName = "" }), validMessage())
	from, _ = mail.ParseAddress(parse(t, out.Raw).Header.Get("From"))
	if from == nil || from.Name != DefaultFromName {
		t.Errorf("From sans nom configuré : %v, attendu le nom par défaut", from)
	}

	// Enveloppe sur un sous-domaine : aligné.
	out = build(t, newBuilder(t, func(o *Options) { o.EnvelopeFrom = "rebond@bounce.example.org" }), validMessage())
	if out.EnvelopeFrom != "rebond@bounce.example.org" {
		t.Errorf("expéditeur d'enveloppe %q", out.EnvelopeFrom)
	}

	// Enveloppe sur un autre domaine : refusé.
	o := testOptions()
	o.EnvelopeFrom = "rebond@autre-domaine.example"
	_, err = New(o)
	assertKind(t, err, ErrAddress)
	o.EnvelopeFrom = "rebond@notexample.org" // suffixe sans point : pas un sous-domaine
	_, err = New(o)
	assertKind(t, err, ErrAddress)
}

func TestHeadersDateRFC5322FromClock(t *testing.T) {
	out := build(t, newBuilder(t, nil), validMessage())
	msg := parse(t, out.Raw)
	if got, want := msg.Header.Get("Date"), "Tue, 01 Dec 2026 09:30:15 +0000"; got != want {
		t.Errorf("Date = %q, attendu %q", got, want)
	}
	d, err := msg.Header.Date()
	if err != nil {
		t.Fatalf("Date illisible : %v", err)
	}
	if !d.Equal(testNow) {
		t.Errorf("Date = %v, attendu %v", d, testNow)
	}

	paris, err := time.LoadLocation("Europe/Paris")
	if err != nil {
		t.Skip("fuseau indisponible")
	}
	b := newBuilder(t, func(o *Options) { o.Clock = fixedClock(testNow.In(paris)) })
	got := parse(t, build(t, b, validMessage()).Raw).Header.Get("Date")
	if got != "Tue, 01 Dec 2026 10:30:15 +0100" {
		t.Errorf("Date locale = %q", got)
	}
}

type fixedClock time.Time

func (c fixedClock) Now() time.Time { return time.Time(c) }

var messageIDRe = regexp.MustCompile(`^<[0-9a-f]{32}@example\.org>$`)

func TestHeadersMessageIDUniqueAtSenderDomain(t *testing.T) {
	b := newBuilder(t, func(o *Options) { o.Random = nil }) // aléa réel
	seen := map[string]bool{}
	for range 200 {
		out := build(t, b, validMessage())
		id := parse(t, out.Raw).Header.Get("Message-ID")
		if id != out.MessageID {
			t.Fatalf("Built.MessageID %q ≠ en-tête %q", out.MessageID, id)
		}
		if !messageIDRe.MatchString(id) {
			t.Fatalf("Message-ID %q n'a pas la forme <identifiant@domaine-expéditeur>", id)
		}
		if seen[id] {
			t.Fatalf("Message-ID %q répété", id)
		}
		seen[id] = true
	}
	// Même avec un aléa déterministe, deux messages successifs diffèrent.
	b = newBuilder(t, nil)
	a1, a2 := build(t, b, validMessage()), build(t, b, validMessage())
	if a1.MessageID == a2.MessageID {
		t.Errorf("deux messages successifs partagent le Message-ID %s", a1.MessageID)
	}
}

func TestHeadersReplyToOnlyWhenConfigured(t *testing.T) {
	out := build(t, newBuilder(t, func(o *Options) { o.ReplyTo = "organisation@example.org" }), validMessage())
	msg := parse(t, out.Raw)
	if got := msg.Header["Reply-To"]; len(got) != 1 || got[0] != "organisation@example.org" {
		t.Errorf("Reply-To = %v", got)
	}
	o := testOptions()
	o.ReplyTo = "pas une adresse"
	_, err := New(o)
	assertKind(t, err, ErrAddress)
}

func TestHeadersToCarriesOptionalName(t *testing.T) {
	m := validMessage()
	out := build(t, newBuilder(t, nil), m)
	to, err := mail.ParseAddress(parse(t, out.Raw).Header.Get("To"))
	if err != nil || to.Name != "Léa" || to.Address != testTo {
		t.Errorf("To = %v (%v)", to, err)
	}
	m.ToName = ""
	out = build(t, newBuilder(t, nil), m)
	if got := parse(t, out.Raw).Header.Get("To"); got != testTo {
		t.Errorf("To sans nom = %q", got)
	}
}

func TestHeadersLinesFolded(t *testing.T) {
	m := validMessage()
	m.ToName = strings.Repeat("Marie-Éléonore ", 6)
	out := build(t, newBuilder(t, nil), m)
	for _, l := range headerLines(out.Raw) {
		if len(l) > 78 {
			t.Errorf("ligne d'en-tête de %d caractères : %q", len(l), l)
		}
	}
	to, err := mail.ParseAddress(parse(t, out.Raw).Header.Get("To"))
	if err != nil || to.Name != strings.TrimSpace(m.ToName) {
		t.Errorf("To plié illisible : %v (%v)", to, err)
	}
}
