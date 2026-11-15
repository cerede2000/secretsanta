//go:debug rsa1024min=0

package maildkim

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"secretsanta/internal/config"
	"secretsanta/internal/shared/errs"
)

// F6b.AC10 : clé lue seulement depuis un fichier ; fichier illisible, invalide
// ou de type incompatible refusé ; la clé jamais journalisée ni renvoyée dans
// une erreur.

func TestKeyLoadedFromFile(t *testing.T) {
	rsaPEM := pkcs8PEM(t, testRSAKey(t))
	pkcs1 := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(testRSAKey(t))})
	edPEM := pkcs8PEM(t, testEdKey(t))
	for _, c := range []struct {
		name, alg string
		pem       []byte
	}{
		{"rsa pkcs8", config.AlgRSA, rsaPEM},
		{"rsa pkcs1", config.AlgRSA, pkcs1},
		{"ed25519 pkcs8", config.AlgEd25519, edPEM},
	} {
		t.Run(c.name, func(t *testing.T) {
			k, err := LoadKey(config.NewSecret(writeFile(t, c.pem)), c.alg)
			if err != nil || k == nil {
				t.Fatalf("clé valide refusée : %v", err)
			}
		})
	}
}

// Le paramètre de LoadKey et le champ KeyFile sont des chemins : un contenu
// PEM passé à la place est traité comme un chemin introuvable, jamais comme
// une clé.
func TestKeyNeverTakenFromInlineValue(t *testing.T) {
	inline := string(pkcs8PEM(t, testEdKey(t)))
	_, err := LoadKey(config.NewSecret(inline), config.AlgEd25519)
	if !errors.Is(err, ErrKeyUnreadable) {
		t.Fatalf("un contenu PEM en clair doit être refusé comme chemin illisible, obtenu %v", err)
	}
	assertNoLeak(t, err, inline, inline)
	if _, err := New(dkimConfig(inline, config.AlgEd25519)); !errors.Is(err, ErrKeyUnreadable) {
		t.Fatalf("New avec une clé en clair : %v", err)
	}
}

func TestKeyRefusals(t *testing.T) {
	rsaPEM := pkcs8PEM(t, testRSAKey(t))
	edPEM := pkcs8PEM(t, testEdKey(t))
	ec, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	ecPEM := pkcs8PEM(t, ec)
	small, err := rsa.GenerateKey(rand.Reader, 512)
	if err != nil {
		t.Fatal(err)
	}
	smallPEM := pkcs8PEM(t, small)
	corrupt := []byte("-----BEGIN PRIVATE KEY-----\n" + base64.StdEncoding.EncodeToString([]byte("TOPSECRETKEYMATERIAL")) + "\n-----END PRIVATE KEY-----\n")
	encrypted := []byte("-----BEGIN RSA PRIVATE KEY-----\nProc-Type: 4,ENCRYPTED\nDEK-Info: AES-128-CBC,00000000000000000000000000000000\n\nAAAA\n-----END RSA PRIVATE KEY-----\n")
	cert := []byte("-----BEGIN CERTIFICATE-----\nAAAA\n-----END CERTIFICATE-----\n")

	unreadable := writeFile(t, edPEM)
	if err := os.Chmod(unreadable, 0); err != nil {
		t.Fatal(err)
	}
	noPerm := func() bool { f, err := os.Open(unreadable); _ = f.Close(); return err != nil }()

	cases := []struct {
		name, path, alg string
		secret          []byte
		want            error
		skip            bool
	}{
		{"absent", filepath.Join(t.TempDir(), "absent.pem"), config.AlgRSA, nil, ErrKeyUnreadable, false},
		{"répertoire", t.TempDir(), config.AlgRSA, nil, ErrKeyUnreadable, false},
		{"sans droit de lecture", unreadable, config.AlgEd25519, edPEM, ErrKeyUnreadable, !noPerm},
		{"chemin vide", "", config.AlgRSA, nil, ErrKeyUnreadable, false},
		{"pas du PEM", writeFile(t, []byte("pas une cle")), config.AlgRSA, []byte("pas une cle"), ErrKeyInvalid, false},
		{"PEM corrompu", writeFile(t, corrupt), config.AlgRSA, corrupt, ErrKeyInvalid, false},
		{"PEM chiffré", writeFile(t, encrypted), config.AlgRSA, encrypted, ErrKeyInvalid, false},
		{"certificat seul", writeFile(t, cert), config.AlgRSA, cert, ErrKeyInvalid, false},
		{"RSA trop courte", writeFile(t, smallPEM), config.AlgRSA, smallPEM, ErrKeyInvalid, false},
		{"RSA pour ed25519", writeFile(t, rsaPEM), config.AlgEd25519, rsaPEM, ErrKeyAlgorithmMismatch, false},
		{"Ed25519 pour rsa", writeFile(t, edPEM), config.AlgRSA, edPEM, ErrKeyAlgorithmMismatch, false},
		{"ECDSA", writeFile(t, ecPEM), config.AlgRSA, ecPEM, ErrKeyAlgorithmMismatch, false},
		{"algorithme inconnu", writeFile(t, rsaPEM), "rsa-sha1", rsaPEM, ErrUnknownAlgorithm, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if c.skip {
				t.Skip("exécuté en root : un fichier sans droit reste lisible")
			}
			_, err := LoadKey(config.NewSecret(c.path), c.alg)
			if !errors.Is(err, c.want) {
				t.Fatalf("attendu %v, obtenu %v", c.want, err)
			}
			if errs.KindOf(err) != errs.KindInternal {
				t.Fatalf("nature d'erreur inattendue : %v", errs.KindOf(err))
			}
			if !strings.Contains(err.Error(), "DKIM") {
				t.Fatalf("message peu explicite : %q", err.Error())
			}
			assertNoLeak(t, err, c.path, string(c.secret))
			// Même refus par New, sans fuite non plus.
			_, nerr := New(dkimConfig(c.path, c.alg))
			if !errors.Is(nerr, c.want) {
				t.Fatalf("New : attendu %v, obtenu %v", c.want, nerr)
			}
			assertNoLeak(t, nerr, c.path, string(c.secret))
		})
	}
}

// Ni fmt ni slog ne montrent la clé (ni le chemin) d'un signataire actif.
func TestSignerNeverPrintsKey(t *testing.T) {
	for _, kc := range keyCases(t) {
		t.Run(kc.alg, func(t *testing.T) {
			pemBytes := pkcs8PEM(t, kc.priv)
			// Dossier à nom sentinelle : le nom d'un dossier temporaire ("001")
			// peut apparaître par hasard dans un horodatage de journal et
			// rendrait le test instable.
			const sentinelDir = "dossier-sentinelle-zq7x-cle"
			dir := filepath.Join(t.TempDir(), sentinelDir)
			if err := os.Mkdir(dir, 0o700); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(dir, "dkim.pem")
			if err := os.WriteFile(path, pemBytes, 0o600); err != nil {
				t.Fatal(err)
			}
			s, err := New(dkimConfig(path, kc.alg))
			if err != nil {
				t.Fatal(err)
			}
			secrets := keyMaterial(t, kc.priv, pemBytes)
			var out []string
			for _, verb := range []string{"%v", "%+v", "%#v", "%s", "%q", "%x"} {
				out = append(out, fmt.Sprintf(verb, s), fmt.Sprintf(verb, *s))
			}
			var buf bytes.Buffer
			for _, h := range []slog.Handler{slog.NewTextHandler(&buf, nil), slog.NewJSONHandler(&buf, nil)} {
				slog.New(h).Info("dkim", "signer", s, "signer_value", *s)
			}
			out = append(out, buf.String())
			all := strings.Join(out, "\n")
			if !strings.Contains(all, "example.org") {
				t.Fatalf("les paramètres publics devraient rester visibles : %s", all)
			}
			if strings.Contains(all, path) || strings.Contains(all, sentinelDir) {
				t.Fatalf("le chemin de la clé apparaît : %s", all)
			}
			for _, sec := range secrets {
				if strings.Contains(all, sec) {
					t.Fatalf("matériel de clé visible dans la sortie : %s", all)
				}
			}
		})
	}
}

// keyMaterial renvoie des représentations de la clé privée qui ne doivent
// apparaître nulle part.
func keyMaterial(t *testing.T, k any, pemBytes []byte) []string {
	t.Helper()
	der, err := x509.MarshalPKCS8PrivateKey(k)
	if err != nil {
		t.Fatal(err)
	}
	b64 := base64.StdEncoding.EncodeToString(der)
	out := []string{b64[len(b64)/2 : len(b64)/2+24], fmt.Sprintf("%x", der)[40:80], "PRIVATE KEY", string(pemBytes)}
	if r, ok := k.(*rsa.PrivateKey); ok {
		out = append(out, r.D.String()[:24], r.Primes[0].String()[:24])
	}
	return out
}

func assertNoLeak(t *testing.T, err error, path, secret string) {
	t.Helper()
	if err == nil {
		return
	}
	var texts []string
	for e := err; e != nil; e = errors.Unwrap(e) {
		texts = append(texts, e.Error(), fmt.Sprintf("%+v", e), fmt.Sprintf("%#v", e))
	}
	all := strings.Join(texts, "\n")
	if path != "" && strings.Contains(all, path) {
		t.Fatalf("l'erreur révèle le chemin de la clé : %s", all)
	}
	if strings.Contains(all, "TOPSECRET") || strings.Contains(all, "PRIVATE KEY") {
		t.Fatalf("l'erreur révèle le contenu de la clé : %s", all)
	}
	if secret != "" {
		for _, line := range strings.Split(secret, "\n") {
			if len(line) >= 16 && strings.Contains(all, line) {
				t.Fatalf("l'erreur révèle une ligne du fichier : %s", all)
			}
		}
	}
}
