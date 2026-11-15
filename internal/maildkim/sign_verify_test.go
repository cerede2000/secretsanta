package maildkim

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"reflect"
	"strings"
	"testing"

	"github.com/emersion/go-msgauth/dkim"

	"secretsanta/internal/config"
)

// F6b.AC9 : signature rsa-sha256 et ed25519-sha256 relaxed/relaxed vérifiable
// par un vérificateur indépendant, en-têtes signés = liste configurée, hachage
// du corps correct.
func TestSignVerifiesWithIndependentVerifier(t *testing.T) {
	for _, kc := range keyCases(t) {
		t.Run(kc.alg, func(t *testing.T) {
			s := newSigner(t, kc)
			msg := sampleMessage("noel@example.org")
			signed, err := s.Sign(msg)
			if err != nil {
				t.Fatalf("Sign : %v", err)
			}
			if !bytes.HasPrefix(signed, []byte("DKIM-Signature:")) {
				t.Fatalf("l'en-tête DKIM-Signature doit être placé en tête :\n%s", signed)
			}
			if !bytes.HasSuffix(signed, msg) {
				t.Fatal("le message d'origine doit suivre l'en-tête de signature sans modification")
			}
			v, err := independentVerify(signed, kc.priv.Public())
			if err != nil {
				t.Fatalf("vérification indépendante : %v\n%s", err, signed)
			}
			if v.Algorithm != kc.alg || v.Domain != "example.org" || v.Selector != "noel2026" || v.Canon != "relaxed/relaxed" {
				t.Fatalf("étiquettes inattendues : %+v", v)
			}
			want := strings.Split(config.DefaultDKIMHeaders, ",")
			if !equalFoldList(v.Headers, want) {
				t.Fatalf("h= = %v, liste configurée %v", v.Headers, want)
			}
			// Hachage du corps recalculé à la main sur ce corps connu.
			body := "--frontiere\r\nContent-Type: text/plain; charset=UTF-8\r\n" +
				"Content-Transfer-Encoding: quoted-printable\r\n\r\nBonjour Alice,\r\n" +
				"Vous offrez un cadeau =C3=A0 Bob.\r\n--frontiere--\r\n"
			sum := sha256.Sum256([]byte(body))
			if v.BodyHash != base64.StdEncoding.EncodeToString(sum[:]) {
				t.Fatalf("bh= %s ne correspond pas au corps canonicalisé", v.BodyHash)
			}
		})
	}
}

func TestSignedHeadersFollowConfiguredList(t *testing.T) {
	custom := []string{"From", "To", "Subject", "Date", "Message-ID", "MIME-Version", "List-Unsubscribe"}
	for _, kc := range keyCases(t) {
		t.Run(kc.alg, func(t *testing.T) {
			s := newSigner(t, kc, custom...)
			signed, err := s.Sign(sampleMessage("noel@example.org"))
			if err != nil {
				t.Fatal(err)
			}
			v, err := independentVerify(signed, kc.priv.Public())
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(v.Headers, custom) {
				t.Fatalf("h= = %v, attendu %v", v.Headers, custom)
			}
			// Un en-tête hors liste peut changer sans casser la signature…
			outside := bytes.Replace(signed, []byte("Content-Language: fr"), []byte("Content-Language: en"), 1)
			if _, err := independentVerify(outside, kc.priv.Public()); err != nil {
				t.Fatalf("en-tête non signé modifié : la signature devait rester valide : %v", err)
			}
			// …mais pas un en-tête de la liste.
			for _, tamper := range [][2]string{
				{"Message-ID: <abc123@example.org>", "Message-ID: <autre@example.org>"},
				{"To: Alice <alice@example.net>", "To: Mallory <mallory@example.net>"},
				{"https://noel.example.org/desinscription/xyz", "https://evil.example/xyz"},
			} {
				bad := bytes.Replace(signed, []byte(tamper[0]), []byte(tamper[1]), 1)
				if bytes.Equal(bad, signed) {
					t.Fatalf("remplacement sans effet : %q", tamper[0])
				}
				if _, err := independentVerify(bad, kc.priv.Public()); err == nil {
					t.Fatalf("en-tête signé modifié (%s) : la vérification devait échouer", tamper[0])
				}
			}
		})
	}
}

func TestBodyHashDetectsTampering(t *testing.T) {
	for _, kc := range keyCases(t) {
		t.Run(kc.alg, func(t *testing.T) {
			signed, err := newSigner(t, kc).Sign(sampleMessage("noel@example.org"))
			if err != nil {
				t.Fatal(err)
			}
			bad := bytes.Replace(signed, []byte("Bob"), []byte("Eve"), 1)
			_, err = independentVerify(bad, kc.priv.Public())
			if err == nil || !strings.Contains(err.Error(), "bh=") {
				t.Fatalf("corps modifié : échec sur bh= attendu, obtenu %v", err)
			}
		})
	}
}

// La canonicalisation relaxed tolère le repliage et les espaces ajoutés en
// transit, ce que la canonicalisation simple refuserait.
func TestRelaxedCanonicalizationToleratesWhitespace(t *testing.T) {
	for _, kc := range keyCases(t) {
		t.Run(kc.alg, func(t *testing.T) {
			signed, err := newSigner(t, kc).Sign(sampleMessage("noel@example.org"))
			if err != nil {
				t.Fatal(err)
			}
			moved := bytes.Replace(signed, []byte("To: Alice <alice@example.net>"), []byte("TO:   Alice\r\n\t<alice@example.net>  "), 1)
			moved = bytes.Replace(moved, []byte("Bonjour  Alice,"), []byte("Bonjour \t Alice,  "), 1)
			moved = append(moved, []byte("\r\n\r\n")...)
			if _, err := independentVerify(moved, kc.priv.Public()); err != nil {
				t.Fatalf("relaxed/relaxed doit tolérer ces espaces : %v", err)
			}
		})
	}
}

func TestIndependentVerifierRejectsWrongKey(t *testing.T) {
	signed, err := newSigner(t, keyCase{config.AlgEd25519, testEdKey(t)}).Sign(sampleMessage("noel@example.org"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := independentVerify(signed, testEdKey(t).Public()); err == nil {
		t.Fatal("une autre clé publique ne doit pas vérifier la signature")
	}
}

func TestBareLFMessageIsSignedInCRLF(t *testing.T) {
	kc := keyCase{config.AlgEd25519, testEdKey(t)}
	lf := bytes.ReplaceAll(sampleMessage("noel@example.org"), []byte("\r\n"), []byte("\n"))
	signed, err := newSigner(t, kc).Sign(lf)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(bytes.ReplaceAll(signed, []byte("\r\n"), nil), []byte("\n")) {
		t.Fatal("le message signé ne doit plus contenir de LF nu")
	}
	if _, err := independentVerify(signed, kc.priv.Public()); err != nil {
		t.Fatalf("vérification indépendante : %v", err)
	}
}

// Contrôle croisé avec un second vérificateur (go-msgauth), clé publique
// publiée dans un enregistrement TXT simulé.
func TestSignatureAlsoVerifiesWithDNSRecord(t *testing.T) {
	for _, kc := range keyCases(t) {
		t.Run(kc.alg, func(t *testing.T) {
			signed, err := newSigner(t, kc).Sign(sampleMessage("noel@example.org"))
			if err != nil {
				t.Fatal(err)
			}
			var record string
			switch pub := kc.priv.Public().(type) {
			case *rsa.PublicKey:
				der, err := x509.MarshalPKIXPublicKey(pub)
				if err != nil {
					t.Fatal(err)
				}
				record = "v=DKIM1; k=rsa; p=" + base64.StdEncoding.EncodeToString(der)
			case ed25519.PublicKey:
				record = "v=DKIM1; k=ed25519; p=" + base64.StdEncoding.EncodeToString(pub)
			}
			vs, err := dkim.VerifyWithOptions(bytes.NewReader(signed), &dkim.VerifyOptions{
				LookupTXT: func(name string) ([]string, error) {
					if name != "noel2026._domainkey.example.org" {
						t.Fatalf("requête DNS inattendue : %s", name)
					}
					return []string{record}, nil
				},
			})
			if err != nil || len(vs) != 1 || vs[0].Err != nil {
				t.Fatalf("vérification DNS : %v %+v", err, vs)
			}
		})
	}
}

func equalFoldList(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if !strings.EqualFold(strings.TrimSpace(a[i]), strings.TrimSpace(b[i])) {
			return false
		}
	}
	return true
}
