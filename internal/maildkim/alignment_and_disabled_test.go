package maildkim

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"secretsanta/internal/config"
	"secretsanta/internal/shared/errs"
)

// F6b.AC11 : domaine de signature aligné sur l'adresse d'expédition, sinon
// refus explicite ; sans configuration DKIM, message intact.

func TestAlignedDomainsAreSigned(t *testing.T) {
	for _, kc := range keyCases(t) {
		t.Run(kc.alg, func(t *testing.T) {
			s := newSigner(t, kc)
			for _, from := range []string{"noel@example.org", "noel@EXAMPLE.org", "noel@mail.example.org"} {
				signed, err := s.Sign(sampleMessage(from))
				if err != nil {
					t.Fatalf("%s : signature refusée à tort : %v", from, err)
				}
				if _, err := independentVerify(signed, kc.priv.Public()); err != nil {
					t.Fatalf("%s : %v", from, err)
				}
			}
		})
	}
}

func TestMisalignedDomainIsRefused(t *testing.T) {
	for _, kc := range keyCases(t) {
		t.Run(kc.alg, func(t *testing.T) {
			s := newSigner(t, kc)
			for _, from := range []string{
				"noel@example.net",
				"noel@notexample.org", // suffixe sans point : pas un sous-domaine
				"noel@example.org.evil.test",
				"noel@org",
			} {
				out, err := s.Sign(sampleMessage(from))
				if out != nil {
					t.Fatalf("%s : aucun message ne doit être rendu en cas de refus", from)
				}
				if !errors.Is(err, ErrNotAligned) || errs.KindOf(err) != errs.KindValidation {
					t.Fatalf("%s : refus d'alignement attendu, obtenu %v", from, err)
				}
				msg := err.Error()
				domain := from[strings.IndexByte(from, '@')+1:]
				if !strings.Contains(msg, "example.org") || !strings.Contains(msg, domain) || !strings.Contains(msg, "align") {
					t.Fatalf("%s : message d'erreur peu explicite : %q", from, msg)
				}
			}
		})
	}
}

func TestUnusableFromIsRefused(t *testing.T) {
	s := newSigner(t, keyCase{config.AlgEd25519, testEdKey(t)})
	base := sampleMessage("noel@example.org")
	fromLine := []byte("From: =?UTF-8?Q?P=C3=A8re_No=C3=ABl?= <noel@example.org>\r\n")
	for name, msg := range map[string][]byte{
		"sans From":        bytes.Replace(base, fromLine, nil, 1),
		"deux From":        append(append([]byte{}, fromLine...), base...),
		"deux adresses":    bytes.Replace(base, fromLine, []byte("From: a@example.org, b@example.org\r\n"), 1),
		"adresse invalide": bytes.Replace(base, fromLine, []byte("From: pas une adresse\r\n"), 1),
		"en-têtes cassés":  []byte("pas d'en-tete\r\n"),
	} {
		out, err := s.Sign(msg)
		if err == nil || out != nil {
			t.Fatalf("%s : la signature devait être refusée", name)
		}
		if !errors.Is(err, ErrBadMessage) {
			t.Fatalf("%s : %v", name, err)
		}
	}
}

func TestDisabledLeavesMessageIntact(t *testing.T) {
	// Configuration absente (valeur zéro, comme config.Load sans variable DKIM),
	// même avec des champs résiduels : Enabled() décide, aucun fichier lu.
	for name, cfg := range map[string]config.DKIM{
		"vide":                {},
		"sans domaine":        {Selector: "x", KeyFile: config.NewSecret("/nexiste/pas.pem"), Algorithm: config.AlgRSA},
		"chargée sans DKIM":   loadedWithoutDKIM(t),
		"chemin vers le vide": {KeyFile: config.NewSecret("/nexiste/pas.pem")},
	} {
		t.Run(name, func(t *testing.T) {
			s, err := New(cfg)
			if err != nil {
				t.Fatalf("New sans configuration DKIM : %v", err)
			}
			if s.Enabled() {
				t.Fatal("le signataire doit être désactivé")
			}
			for _, msg := range [][]byte{
				sampleMessage("noel@example.org"),
				sampleMessage("noel@autre-domaine.test"), // pas d'alignement à vérifier
				[]byte("message\nnon conforme\n"),        // ni normalisation
			} {
				orig := append([]byte(nil), msg...)
				out, err := s.Sign(msg)
				if err != nil {
					t.Fatalf("désactivé : erreur inattendue %v", err)
				}
				if !bytes.Equal(out, orig) || !bytes.Equal(msg, orig) {
					t.Fatalf("désactivé : message modifié\n%q\n%q", orig, out)
				}
			}
		})
	}
	var nilSigner *Signer
	msg := sampleMessage("noel@example.org")
	if out, err := nilSigner.Sign(msg); err != nil || !bytes.Equal(out, msg) || nilSigner.Enabled() {
		t.Fatal("un signataire nil doit laisser le message intact")
	}
}

func loadedWithoutDKIM(t *testing.T) config.DKIM {
	t.Helper()
	env := map[string]string{
		config.VarInstancePassword: "mot-de-passe-d-instance-de-test-assez-long",
		config.VarSigningSecret:    "secret-de-signature-de-test-assez-long-0123456789abcdef",
	}
	c, err := config.Load(func(k string) string { return env[k] })
	if err != nil {
		t.Fatalf("configuration minimale : %v", err)
	}
	return c.DKIM
}

func TestConfiguredDomainIsNormalised(t *testing.T) {
	kc := keyCase{config.AlgEd25519, testEdKey(t)}
	cfg := dkimConfig(writeFile(t, pkcs8PEM(t, kc.priv)), kc.alg)
	cfg.Domain = "Example.ORG."
	s, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	signed, err := s.Sign(sampleMessage("noel@example.org"))
	if err != nil {
		t.Fatalf("domaine configuré en majuscules avec point final : %v", err)
	}
	if v, err := independentVerify(signed, kc.priv.Public()); err != nil || v.Domain != "example.org" {
		t.Fatalf("d= attendu example.org : %+v %v", v, err)
	}
}
