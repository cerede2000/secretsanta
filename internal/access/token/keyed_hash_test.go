package token

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"testing"

	"secretsanta/internal/shared/errs"

	"pgregory.net/rapid"
)

// framed reproduit, indépendamment de Hash, le message authentifié : chaque
// partie est précédée de sa longueur sur 8 octets gros-boutistes.
func framed(parts ...string) []byte {
	var buf bytes.Buffer
	for _, p := range parts {
		var n [8]byte
		binary.BigEndian.PutUint64(n[:], uint64(len(p)))
		buf.Write(n[:])
		buf.WriteString(p)
	}
	return buf.Bytes()
}

func refHMAC(key []byte, msg []byte) string {
	m := hmac.New(sha256.New, key)
	m.Write(msg)
	return hex.EncodeToString(m.Sum(nil))
}

func plainSHA256(msg []byte) string {
	s := sha256.Sum256(msg)
	return hex.EncodeToString(s[:])
}

var kinds = []Kind{KindLink, KindCode}

// hmacEquivalent rapporte si deux clés sont la même clé pour HMAC : une clé
// d'au plus 64 octets est complétée par des zéros, si bien que des zéros
// finaux n'en changent pas la valeur (propriété de HMAC, pas du paquet ; une
// variable d'environnement ne peut pas contenir d'octet nul).
func hmacEquivalent(a, b []byte) bool {
	if len(a) > sha256.BlockSize || len(b) > sha256.BlockSize {
		return bytes.Equal(a, b)
	}
	return bytes.Equal(bytes.TrimRight(a, "\x00"), bytes.TrimRight(b, "\x00"))
}

// L'empreinte est exactement HMAC-SHA-256(clé, domaine‖groupe‖nature‖secret),
// et non un SHA-256 sans clé.
func TestHashIsHMACSHA256WithSigningKey(t *testing.T) {
	if hashDomain != "secretsanta/access/token/v2" {
		t.Fatalf("domaine %q inattendu", hashDomain)
	}
	rapid.Check(t, func(t *rapid.T) {
		key := rapid.SliceOfN(rapid.Byte(), 1, 96).Draw(t, "key")
		group := rapid.String().Draw(t, "group")
		kind := rapid.SampledFrom(kinds).Draw(t, "kind")
		secret := rapid.String().Draw(t, "secret")

		msg := framed(hashDomain, group, string(kind), secret)
		got := Hash(key, group, kind, secret)
		if want := refHMAC(key, msg); got != want {
			t.Fatalf("Hash = %s, HMAC-SHA-256 attendu %s", got, want)
		}
		if len(got) != 64 {
			t.Fatalf("empreinte de %d caractères, attendu 64", len(got))
		}
		if got == plainSHA256(msg) || got == plainSHA256(framed("secretsanta/access/token/v1", group, string(kind), secret)) {
			t.Fatal("l'empreinte est un SHA-256 sans clé")
		}
		if !hmacEquivalent(key, nil) && got == refHMAC(nil, msg) {
			t.Fatal("l'empreinte ne dépend pas de la clé")
		}
	})
}

// Une fuite de la base livre les empreintes, le groupe et la nature, mais pas
// la clé. Même en connaissant le code exact (meilleur cas pour l'attaquant),
// aucune clé autre que la clé de signature, ni un SHA-256 sans clé, ne
// reproduit l'empreinte : le code n'est pas vérifiable donc pas retrouvable
// hors ligne.
func TestEightCharCodeNotRecoverableWithoutKey(t *testing.T) {
	rec := &recordingStore{MemoryStore: NewMemoryStore()}
	svc, err := NewService(rec, LoginCodeLength, testKey)
	if err != nil {
		t.Fatal(err)
	}
	code, err := svc.IssueCode(context.Background(), "g1", "alice")
	if err != nil {
		t.Fatal(err)
	}
	if len(code) != 8 || len(rec.puts) != 1 {
		t.Fatalf("code %q, %d écritures", code, len(rec.puts))
	}
	stored := rec.puts[0].Hash
	if Hash(testKey, "g1", KindCode, code) != stored {
		t.Fatal("la clé de signature ne reproduit pas l'empreinte")
	}
	for _, guess := range []string{
		plainSHA256(framed(hashDomain, "g1", string(KindCode), code)),
		plainSHA256(framed("secretsanta/access/token/v1", "g1", string(KindCode), code)),
		plainSHA256([]byte(code)),
		refHMAC(nil, framed(hashDomain, "g1", string(KindCode), code)),
		refHMAC([]byte(code), framed(hashDomain, "g1", string(KindCode), code)),
	} {
		if guess == stored {
			t.Fatal("empreinte reproduite sans la clé de signature")
		}
	}
	rapid.Check(t, func(t *rapid.T) {
		other := rapid.SliceOfN(rapid.Byte(), 0, 96).
			Filter(func(k []byte) bool { return !hmacEquivalent(k, testKey) }).Draw(t, "otherKey")
		if Hash(other, "g1", KindCode, code) == stored {
			t.Fatalf("la clé %x reproduit l'empreinte", other)
		}
	})
}

// Démonstration exhaustive sur l'espace entier des codes de 4 caractères
// (31^4 ≈ 924 000 codes) : avec la clé, l'énumération retrouve le code ;
// avec une autre clé, aucun code de l'espace ne donne l'empreinte. Pour 8
// caractères le raisonnement est le même, l'espace étant seulement plus grand.
func TestExhaustiveEnumerationNeedsTheKey(t *testing.T) {
	if testing.Short() {
		t.Skip("énumération exhaustive ignorée en mode court")
	}
	const n = MinCodeLength
	code, err := GenerateCode(n)
	if err != nil {
		t.Fatal(err)
	}
	stored := Hash(testKey, "g1", KindCode, code)
	wrongKey := []byte("une-autre-cle-de-signature")
	var foundRight, foundWrong []string
	buf := make([]byte, n)
	var walk func(i int)
	walk = func(i int) {
		if i == n {
			c := string(buf)
			if Hash(testKey, "g1", KindCode, c) == stored {
				foundRight = append(foundRight, c)
			}
			if Hash(wrongKey, "g1", KindCode, c) == stored {
				foundWrong = append(foundWrong, c)
			}
			return
		}
		for j := 0; j < len(Alphabet); j++ {
			buf[i] = Alphabet[j]
			walk(i + 1)
		}
	}
	walk(0)
	if len(foundRight) != 1 || foundRight[0] != code {
		t.Fatalf("avec la clé : %q, attendu [%q]", foundRight, code)
	}
	if len(foundWrong) != 0 {
		t.Fatalf("sans la clé, codes retrouvés : %q", foundWrong)
	}
}

// Un service construit avec une autre clé, sur le même magasin, ne résout
// aucun jeton ni code et refuse comme pour un secret inconnu.
func TestOtherKeyResolvesNothing(t *testing.T) {
	ctx := context.Background()
	st := NewMemoryStore()
	svcA, err := NewService(st, LoginCodeLength, testKey)
	if err != nil {
		t.Fatal(err)
	}
	svcB, err := NewService(st, LoginCodeLength, []byte("cle-de-signature-differente"))
	if err != nil {
		t.Fatal(err)
	}
	tok, _ := svcA.IssueLink(ctx, "g1", "alice")
	code, _ := svcA.IssueCode(ctx, "g1", "alice")

	_, errLink := svcB.ResolveLink(ctx, "g1", tok)
	_, errCode := svcB.ResolveCode(ctx, "g1", code)
	_, errUnknown := svcA.ResolveCode(ctx, "g1", "ZZZZ2222")
	for _, err := range []error{errLink, errCode} {
		if !errs.Is(err, errs.KindUnauthorized) {
			t.Fatalf("autre clé : %v", err)
		}
		if errs.UserMessage(err) != errs.UserMessage(errUnknown) {
			t.Fatalf("refus distinguable : %q / %q", errs.UserMessage(err), errs.UserMessage(errUnknown))
		}
	}
	if p, err := svcA.ResolveLink(ctx, "g1", tok); err != nil || p != "alice" {
		t.Fatalf("bonne clé, jeton : %q, %v", p, err)
	}
	if p, err := svcA.ResolveCode(ctx, "g1", code); err != nil || p != "alice" {
		t.Fatalf("bonne clé, code : %q, %v", p, err)
	}

	rapid.Check(t, func(t *rapid.T) {
		other := rapid.SliceOfN(rapid.Byte(), 1, 64).
			Filter(func(k []byte) bool { return !hmacEquivalent(k, testKey) }).Draw(t, "otherKey")
		svc, err := NewService(st, LoginCodeLength, other)
		if err != nil {
			t.Fatal(err)
		}
		if p, err := svc.ResolveLink(ctx, "g1", tok); err == nil {
			t.Fatalf("jeton résolu avec la clé %x (%q)", other, p)
		}
		if p, err := svc.ResolveCode(ctx, "g1", code); err == nil {
			t.Fatalf("code résolu avec la clé %x (%q)", other, p)
		}
	})
}

// La séparation de domaine tient avec la clé : groupe, nature, découpage et
// étiquette de domaine entrent tous dans l'empreinte.
func TestKeyedHashKeepsDomainSeparation(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		key := rapid.SliceOfN(rapid.Byte(), 1, 64).Draw(t, "key")
		a := rapid.String().Draw(t, "groupA")
		b := rapid.String().Filter(func(s string) bool { return s != a }).Draw(t, "groupB")
		secret := rapid.String().Draw(t, "secret")
		kind := rapid.SampledFrom(kinds).Draw(t, "kind")
		if Hash(key, a, kind, secret) == Hash(key, b, kind, secret) {
			t.Fatalf("même empreinte pour les groupes %q et %q", a, b)
		}
		if Hash(key, a, KindLink, secret) == Hash(key, a, KindCode, secret) {
			t.Fatal("la nature n'entre pas dans l'empreinte")
		}
		// Le même HMAC sans l'étiquette de domaine, ou avec une autre, diffère.
		got := Hash(key, a, kind, secret)
		if got == refHMAC(key, framed(a, string(kind), secret)) ||
			got == refHMAC(key, framed("secretsanta/autre-usage", a, string(kind), secret)) {
			t.Fatal("l'étiquette de domaine n'entre pas dans l'empreinte")
		}
	})
	if Hash(testKey, "ab", KindLink, "c") == Hash(testKey, "a", KindLink, "bc") {
		t.Fatal("concaténation ambiguë dans l'empreinte")
	}
	if Hash(testKey, "g", KindLink, "x") == Hash(testKey, "glink", Kind(""), "x") {
		t.Fatal("concaténation ambiguë entre groupe et nature")
	}
}

func TestNewServiceRequiresKey(t *testing.T) {
	for _, key := range [][]byte{nil, {}} {
		if _, err := NewService(NewMemoryStore(), LoginCodeLength, key); !errors.Is(err, ErrMissingKey) {
			t.Fatalf("clé %v : erreur %v, attendu ErrMissingKey", key, err)
		}
	}
}

// Le service copie la clé : modifier ensuite la tranche de l'appelant ne
// change pas les empreintes.
func TestServiceCopiesKey(t *testing.T) {
	ctx := context.Background()
	key := append([]byte(nil), testKey...)
	svc, err := NewService(NewMemoryStore(), LoginCodeLength, key)
	if err != nil {
		t.Fatal(err)
	}
	tok, _ := svc.IssueLink(ctx, "g", "p")
	for i := range key {
		key[i] = 0
	}
	if p, err := svc.ResolveLink(ctx, "g", tok); err != nil || p != "p" {
		t.Fatalf("clé non copiée : %q, %v", p, err)
	}
}
