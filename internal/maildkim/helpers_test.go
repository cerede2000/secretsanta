package maildkim

import (
	"crypto"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"secretsanta/internal/config"
)

var (
	rsaOnce sync.Once
	rsaKey  *rsa.PrivateKey
)

// testRSAKey génère une seule clé RSA 2048 pour tout le paquet.
func testRSAKey(t *testing.T) *rsa.PrivateKey {
	t.Helper()
	rsaOnce.Do(func() {
		k, err := rsa.GenerateKey(rand.Reader, 2048)
		if err != nil {
			panic(err)
		}
		rsaKey = k
	})
	return rsaKey
}

func testEdKey(t *testing.T) ed25519.PrivateKey {
	t.Helper()
	_, k, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	return k
}

func pkcs8PEM(t *testing.T, k any) []byte {
	t.Helper()
	der, err := x509.MarshalPKCS8PrivateKey(k)
	if err != nil {
		t.Fatal(err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der})
}

func writeFile(t *testing.T, content []byte) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "dkim.pem")
	if err := os.WriteFile(p, content, 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func dkimConfig(path, alg string, headers ...string) config.DKIM {
	if len(headers) == 0 {
		headers = strings.Split(config.DefaultDKIMHeaders, ",")
	}
	return config.DKIM{
		Domain:    "example.org",
		Selector:  "noel2026",
		KeyFile:   config.NewSecret(path),
		Algorithm: alg,
		Headers:   headers,
	}
}

type keyCase struct {
	alg  string
	priv crypto.Signer
}

func keyCases(t *testing.T) []keyCase {
	return []keyCase{
		{config.AlgRSA, testRSAKey(t)},
		{config.AlgEd25519, testEdKey(t)},
	}
}

func newSigner(t *testing.T, kc keyCase, headers ...string) *Signer {
	t.Helper()
	s, err := New(dkimConfig(writeFile(t, pkcs8PEM(t, kc.priv)), kc.alg, headers...))
	if err != nil {
		t.Fatalf("New(%s) : %v", kc.alg, err)
	}
	if !s.Enabled() {
		t.Fatal("signataire configuré mais désactivé")
	}
	return s
}

// sampleMessage ressemble à ce que produit le constructeur de messages :
// en-têtes repliés, espaces multiples et lignes vides finales pour exercer
// la canonicalisation relaxed.
func sampleMessage(from string) []byte {
	return []byte(strings.Join([]string{
		"From: =?UTF-8?Q?P=C3=A8re_No=C3=ABl?= <" + from + ">",
		"To: Alice <alice@example.net>",
		"Subject: =?UTF-8?Q?Votre_tirage_est_pr=C3=AAt?=",
		"Date: Sun, 15 Nov 2026 10:00:00 +0100",
		"Message-ID: <abc123@example.org>",
		"MIME-Version: 1.0",
		"Content-Type: multipart/alternative;",
		"\tboundary=\"frontiere\"",
		"List-Unsubscribe: <https://noel.example.org/desinscription/xyz>,",
		"   <mailto:desinscription@example.org?subject=xyz>",
		"List-Unsubscribe-Post: List-Unsubscribe=One-Click",
		"Content-Language: fr",
		"",
		"--frontiere",
		"Content-Type: text/plain; charset=UTF-8",
		"Content-Transfer-Encoding: quoted-printable",
		"",
		"Bonjour  Alice,\t ",
		"Vous offrez un cadeau =C3=A0 Bob.   ",
		"--frontiere--",
		"",
		"",
		"",
	}, "\r\n"))
}
