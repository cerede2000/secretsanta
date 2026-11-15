package templates

import (
	"bytes"
	"io"
	"math/rand/v2"
	"mime"
	"mime/multipart"
	"mime/quotedprintable"
	"net/mail"
	"strings"
	"testing"
	"time"

	"secretsanta/internal/i18n/catalog"
	"secretsanta/internal/mailmsg"
	"secretsanta/internal/shared/clock"
	"secretsanta/web"
)

const (
	testPublicURL = "https://noel.example.org"
	testFrom      = "pere-noel@example.org"
	testHost      = "noel.example.org"
	personalLink  = "https://noel.example.org/famille/p/abc123def456"
	letterLink    = "https://noel.example.org/famille/lettre/abc123def456"
	confirmLink   = "https://noel.example.org/famille/inscription/confirmer/zz99yy88"
	unsubLink     = "https://noel.example.org/desinscription/relances/tok.sig"
)

var testNow = time.Date(2026, 12, 1, 9, 30, 15, 0, time.UTC)

func testCatalog(t *testing.T) *catalog.Catalog {
	t.Helper()
	cat, err := catalog.Load(web.I18n, catalog.Options{DefaultLang: "fr"})
	if err != nil {
		t.Fatalf("catalogue : %v", err)
	}
	return cat
}

func testBuilder(t *testing.T) *mailmsg.Builder {
	t.Helper()
	var seed [32]byte
	seed[0] = 7
	b, err := mailmsg.New(mailmsg.Options{
		PublicURL: testPublicURL, From: testFrom, FromName: "Père Noël secret",
		Clock: clock.NewFake(testNow), Random: rand.NewChaCha8(seed),
	})
	if err != nil {
		t.Fatalf("constructeur : %v", err)
	}
	return b
}

func budget(c int64) *int64 { return &c }

// sampleData renvoie des données complètes et valides pour un modèle.
func sampleData(k Kind) Data {
	d := Data{
		To: "lea@example.net", FirstName: "Léa", GroupName: "Famille Martin",
		Link: personalLink, Code: "K7M2QX", BudgetCents: budget(2500),
	}
	switch k {
	case ReadyReception:
		d.Link = ""
		d.LetterLink = letterLink
	case Verification:
		d.Link = confirmLink
		d.Code = "A1B2C3"
	case LetterUpdated:
		d.Ideas = 3
	case BudgetUpdated:
		d.Link = ""
	}
	if info, _ := InfoOf(k); !info.Essential {
		d.UnsubscribeURL = unsubLink
		if info.UnsubType == UnsubNouvelleIdee {
			d.UnsubscribeURL = "https://noel.example.org/desinscription/nouvelle_idee/tok.sig"
		}
	}
	return d
}

func render(t *testing.T, cat *catalog.Catalog, k Kind, d Data) mailmsg.Message {
	t.Helper()
	m, err := Render(cat, k, d)
	if err != nil {
		t.Fatalf("Render(%s) : %v", k, err)
	}
	return m
}

func buildKind(t *testing.T, cat *catalog.Catalog, k Kind, d Data) (mailmsg.Message, *mailmsg.Built) {
	t.Helper()
	m := render(t, cat, k, d)
	out, err := testBuilder(t).Build(m)
	if err != nil {
		t.Fatalf("Build(%s) : %v\nobjet : %s\n%s\n%s", k, err, m.Subject, m.Text, m.HTML)
	}
	return m, out
}

func parseRaw(t *testing.T, raw []byte) *mail.Message {
	t.Helper()
	msg, err := mail.ReadMessage(bytes.NewReader(raw))
	if err != nil {
		t.Fatalf("message illisible : %v", err)
	}
	return msg
}

// decodedParts renvoie le texte et le HTML décodés, ainsi que les corps bruts.
func decodedParts(t *testing.T, raw []byte) (text, htm string, rawBodies []string) {
	t.Helper()
	msg := parseRaw(t, raw)
	mt, params, err := mime.ParseMediaType(msg.Header.Get("Content-Type"))
	if err != nil || mt != "multipart/alternative" {
		t.Fatalf("Content-Type %q : %v", mt, err)
	}
	r := multipart.NewReader(msg.Body, params["boundary"])
	for {
		p, err := r.NextRawPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		body, _ := io.ReadAll(p)
		rawBodies = append(rawBodies, string(body))
		dec, err := io.ReadAll(quotedprintable.NewReader(bytes.NewReader(body)))
		if err != nil {
			t.Fatal(err)
		}
		ct := p.Header.Get("Content-Type")
		switch {
		case strings.HasPrefix(ct, "text/plain"):
			text = string(dec)
		case strings.HasPrefix(ct, "text/html"):
			htm = string(dec)
		}
	}
	return text, htm, rawBodies
}
