package mailmsg

import (
	"bytes"
	"errors"
	"io"
	"math/rand/v2"
	"mime"
	"mime/multipart"
	"net/mail"
	"strings"
	"testing"
	"time"

	"secretsanta/internal/shared/clock"
)

var testNow = time.Date(2026, 12, 1, 9, 30, 15, 0, time.UTC)

const (
	testPublicURL = "https://noel.example.org"
	testFrom      = "pere-noel@example.org"
	testFromName  = "Père Noël secret"
	testTo        = "lea@example.net"
	testLink      = "https://noel.example.org/famille/tirage"
)

// seeded renvoie un aléa déterministe.
func seeded(seed byte) io.Reader {
	var s [32]byte
	s[0] = seed
	return rand.NewChaCha8(s)
}

func testOptions() Options {
	return Options{
		PublicURL: testPublicURL,
		From:      testFrom,
		FromName:  testFromName,
		Clock:     clock.NewFake(testNow),
		Random:    seeded(1),
	}
}

func newBuilder(t *testing.T, mod func(*Options)) *Builder {
	t.Helper()
	o := testOptions()
	if mod != nil {
		mod(&o)
	}
	b, err := New(o)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return b
}

func validMessage() Message {
	return Message{
		To:      testTo,
		ToName:  "Léa",
		Subject: "Le tirage de Noël est fait",
		Text: "Bonjour Léa,\n\nLe tirage de « Noël en famille » est fait. Découvre à qui tu offres un cadeau :\n" +
			testLink + "\n\nÀ bientôt !\n",
		HTML: "<p>Bonjour Léa,</p>\n<p>Le tirage de « Noël en famille » est fait. Découvre à qui tu offres un cadeau :</p>\n" +
			`<p><a href="` + testLink + `">` + testLink + "</a></p>\n<p>À bientôt !</p>",
		Essential: true,
	}
}

func build(t *testing.T, b *Builder, m Message) *Built {
	t.Helper()
	out, err := b.Build(m)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	return out
}

func parse(t *testing.T, raw []byte) *mail.Message {
	t.Helper()
	msg, err := mail.ReadMessage(bytes.NewReader(raw))
	if err != nil {
		t.Fatalf("message illisible : %v\n%s", err, raw)
	}
	return msg
}

type rawPart struct {
	header map[string][]string
	body   []byte // tel qu'encodé sur le fil
}

// rawParts lit les parties sans décodage automatique.
func rawParts(t *testing.T, msg *mail.Message) (string, []rawPart) {
	t.Helper()
	mt, params, err := mime.ParseMediaType(msg.Header.Get("Content-Type"))
	if err != nil {
		t.Fatalf("Content-Type : %v", err)
	}
	if mt != "multipart/alternative" {
		t.Fatalf("type %q, attendu multipart/alternative", mt)
	}
	r := multipart.NewReader(msg.Body, params["boundary"])
	var parts []rawPart
	for {
		p, err := r.NextRawPart()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatalf("partie : %v", err)
		}
		body, err := io.ReadAll(p)
		if err != nil {
			t.Fatalf("lecture partie : %v", err)
		}
		parts = append(parts, rawPart{header: p.Header, body: body})
	}
	return params["boundary"], parts
}

func headerLines(raw []byte) []string {
	head, _, _ := strings.Cut(string(raw), "\r\n\r\n")
	return strings.Split(head, "\r\n")
}

func assertKind(t *testing.T, err, cat error) {
	t.Helper()
	if err == nil {
		t.Fatalf("erreur attendue (%v), aucune", cat)
	}
	if !errors.Is(err, cat) {
		t.Fatalf("erreur %v, attendue de catégorie %v", err, cat)
	}
}
