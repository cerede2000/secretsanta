package transport

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/emersion/go-msgauth/dkim"

	"secretsanta/internal/config"
	"secretsanta/internal/mail/queue"
	"secretsanta/internal/maildkim"
	"secretsanta/internal/mailmsg"
	"secretsanta/internal/shared/clock"
)

// F6.AC31 : signé quand le DKIM est configuré (vérifiable avec la clé publique),
// non signé sans erreur sinon.
func builtMessage(t *testing.T) queue.Envelope {
	t.Helper()
	b, err := mailmsg.New(mailmsg.Options{
		PublicURL: "https://noel.example.org", From: "noel@example.org", FromName: "Père Noël",
		Clock: clock.NewFake(time.Date(2026, 12, 1, 9, 0, 0, 0, time.UTC)),
	})
	if err != nil {
		t.Fatal(err)
	}
	link := "https://noel.example.org/x"
	m, err := b.Build(mailmsg.Message{
		To: "lea@example.net", ToName: "Léa", Subject: "Le tirage est fait", Essential: true,
		Text: "Bonjour Léa,\n\n" + link + "\n", HTML: `<p>Bonjour Léa,</p><p><a href="` + link + `">` + link + `</a></p>`,
	})
	if err != nil {
		t.Fatal(err)
	}
	return queue.Envelope{From: m.EnvelopeFrom, To: m.To, Raw: m.Raw}
}

func TestDKIMSignedAndVerifies(t *testing.T) {
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	der, _ := x509.MarshalPKCS8PrivateKey(priv)
	path := filepath.Join(t.TempDir(), "dkim.pem")
	if err := os.WriteFile(path, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der}), 0o600); err != nil {
		t.Fatal(err)
	}
	signer, err := maildkim.New(config.DKIM{
		Domain: "example.org", Selector: "noel", KeyFile: config.NewSecret(path),
		Algorithm: "ed25519-sha256", Headers: strings.Split(config.DefaultDKIMHeaders, ","),
	})
	if err != nil {
		t.Fatal(err)
	}
	s := newFakeServer(t, "starttls")
	o := baseOpts(s, "starttls")
	o.Signer = signer
	tr, _ := New(o)
	if err := tr.Send(context.Background(), builtMessage(t)); err != nil {
		t.Fatal(err)
	}
	got := s.data[0]
	if !bytes.HasPrefix(got, []byte("DKIM-Signature:")) {
		t.Fatalf("message non signé : %.80s", got)
	}
	txt := "v=DKIM1; k=ed25519; p=" + base64.StdEncoding.EncodeToString(pub)
	vs, err := dkim.VerifyWithOptions(bytes.NewReader(got), &dkim.VerifyOptions{
		LookupTXT: func(string) ([]string, error) { return []string{txt}, nil },
	})
	if err != nil || len(vs) != 1 || vs[0].Err != nil {
		t.Fatalf("vérification : %v %+v", err, vs)
	}

	// Un message déjà signé (par la file) n'est pas signé deux fois.
	pre, _ := signer.Sign(builtMessage(t).Raw)
	e := builtMessage(t)
	e.Raw = pre
	if err := tr.Send(context.Background(), e); err != nil {
		t.Fatal(err)
	}
	if n := strings.Count(string(s.data[1]), "DKIM-Signature:"); n != 1 {
		t.Fatalf("%d signatures", n)
	}
}

func TestWithoutDKIMSentUnsigned(t *testing.T) {
	s := newFakeServer(t, "starttls")
	tr, _ := New(baseOpts(s, "starttls"))
	if err := tr.Send(context.Background(), builtMessage(t)); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(s.data[0]), "DKIM-Signature") {
		t.Fatal("signature inattendue")
	}
	// Signataire désactivé : même résultat.
	dis, _ := maildkim.New(config.DKIM{})
	o := baseOpts(s, "starttls")
	o.Signer = dis
	tr2, _ := New(o)
	if err := tr2.Send(context.Background(), builtMessage(t)); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(s.data[1]), "DKIM-Signature") {
		t.Fatal("signature inattendue")
	}
}
