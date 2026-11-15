package token

import (
	"context"
	"encoding/base64"
	"fmt"
	"strings"
	"testing"

	"pgregory.net/rapid"
)

const ambiguous = "0O1IL"

func TestTokenIs128Bits(t *testing.T) {
	if TokenBits != 128 {
		t.Fatalf("TokenBits = %d, attendu 128", TokenBits)
	}
	seen := map[string]bool{}
	for i := 0; i < 1000; i++ {
		tok, err := NewToken()
		if err != nil {
			t.Fatal(err)
		}
		raw, err := base64.RawURLEncoding.DecodeString(tok)
		if err != nil {
			t.Fatalf("jeton %q non décodable : %v", tok, err)
		}
		if len(raw)*8 != 128 {
			t.Fatalf("jeton de %d bits, attendu 128", len(raw)*8)
		}
		if !ValidToken(tok) {
			t.Fatalf("ValidToken(%q) = false", tok)
		}
		if seen[tok] {
			t.Fatalf("jeton répété : %q", tok)
		}
		seen[tok] = true
	}
}

func TestValidTokenRejectsMalformed(t *testing.T) {
	tok, _ := NewToken()
	for _, bad := range []string{"", tok[:21], tok + "A", strings.Repeat("!", 22), tok[:21] + "="} {
		if ValidToken(bad) {
			t.Errorf("ValidToken(%q) = true", bad)
		}
	}
}

func TestAlphabetExcludesAmbiguous(t *testing.T) {
	if strings.ContainsAny(Alphabet, ambiguous) {
		t.Fatalf("l'alphabet %q contient un caractère ambigu", Alphabet)
	}
	seen := map[rune]bool{}
	for _, r := range Alphabet {
		if seen[r] {
			t.Fatalf("caractère répété dans l'alphabet : %q", r)
		}
		seen[r] = true
		if !(r >= '2' && r <= '9') && !(r >= 'A' && r <= 'Z') {
			t.Fatalf("caractère inattendu dans l'alphabet : %q", r)
		}
	}
	if len(Alphabet) != 31 {
		t.Fatalf("alphabet de %d symboles, attendu 31", len(Alphabet))
	}
}

func TestLoginCodesNeverContainAmbiguousProperty(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		code, err := GenerateCode(LoginCodeLength)
		if err != nil {
			t.Fatal(err)
		}
		if len(code) != 8 || strings.ContainsAny(code, ambiguous) || !ValidCode(code, 8) {
			t.Fatalf("code invalide : %q", code)
		}
	})
}

// Propriété : quel que soit le nombre de participants d'un groupe, chacun
// reçoit un code distinct qui ne mène qu'à lui.
func TestCodesUniqueInGroupProperty(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		n := rapid.IntRange(1, 60).Draw(t, "participants")
		svc, err := NewService(NewMemoryStore(), LoginCodeLength, testKey)
		if err != nil {
			t.Fatal(err)
		}
		ctx := context.Background()
		codes := map[string]string{}
		for i := 0; i < n; i++ {
			p := fmt.Sprintf("p%d", i)
			code, err := svc.IssueCode(ctx, "g1", p)
			if err != nil {
				t.Fatal(err)
			}
			if other, dup := codes[code]; dup {
				t.Fatalf("code %q donné à %s et %s", code, other, p)
			}
			codes[code] = p
		}
		for code, p := range codes {
			got, err := svc.ResolveCode(ctx, "g1", code)
			if err != nil || got != p {
				t.Fatalf("ResolveCode(%q) = %q, %v ; attendu %q", code, got, err, p)
			}
		}
	})
}

// Propriété : le magasin refuse qu'une empreinte appartienne à deux
// participants du même groupe, quel que soit le code.
func TestStoreRejectsDuplicateHashProperty(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		code := rapid.StringOfN(rapid.SampledFrom([]rune(Alphabet)), 8, 8, -1).Draw(t, "code")
		st := NewMemoryStore()
		ctx := context.Background()
		h := Hash(testKey, "g", KindCode, code)
		if err := st.Put(ctx, Credential{GroupID: "g", ParticipantID: "a", Kind: KindCode, Hash: h}); err != nil {
			t.Fatal(err)
		}
		if err := st.Put(ctx, Credential{GroupID: "g", ParticipantID: "b", Kind: KindCode, Hash: h}); err != ErrDuplicate {
			t.Fatalf("doublon accepté : %v", err)
		}
	})
}

// collidingStore force des collisions pour vérifier que le service retire un
// nouveau code au lieu d'écraser celui d'un autre participant.
type collidingStore struct {
	*MemoryStore
	collisions int
}

func (c *collidingStore) Put(ctx context.Context, cr Credential) error {
	if c.collisions > 0 {
		c.collisions--
		return ErrDuplicate
	}
	return c.MemoryStore.Put(ctx, cr)
}

func TestIssueCodeRetriesOnCollision(t *testing.T) {
	st := &collidingStore{MemoryStore: NewMemoryStore(), collisions: 3}
	svc, _ := NewService(st, LoginCodeLength, testKey)
	code, err := svc.IssueCode(context.Background(), "g", "p")
	if err != nil {
		t.Fatal(err)
	}
	if st.collisions != 0 {
		t.Fatal("les collisions n'ont pas été consommées")
	}
	if got, err := svc.ResolveCode(context.Background(), "g", code); err != nil || got != "p" {
		t.Fatalf("ResolveCode = %q, %v", got, err)
	}

	st = &collidingStore{MemoryStore: NewMemoryStore(), collisions: 1 << 30}
	svc, _ = NewService(st, LoginCodeLength, testKey)
	if _, err := svc.IssueCode(context.Background(), "g", "p"); err == nil {
		t.Fatal("collisions infinies : erreur attendue")
	}
}

// Le secret en clair n'est jamais conservé : le magasin ne voit que des
// empreintes qui ne le contiennent pas.
func TestSecretsStoredHashed(t *testing.T) {
	rec := &recordingStore{MemoryStore: NewMemoryStore()}
	svc, _ := NewService(rec, LoginCodeLength, testKey)
	ctx := context.Background()
	tok, err := svc.IssueLink(ctx, "g", "p")
	if err != nil {
		t.Fatal(err)
	}
	code, err := svc.IssueCode(ctx, "g", "p")
	if err != nil {
		t.Fatal(err)
	}
	if len(rec.puts) != 2 {
		t.Fatalf("%d écritures, attendu 2", len(rec.puts))
	}
	for _, c := range rec.puts {
		if strings.Contains(c.Hash, tok) || strings.Contains(c.Hash, code) ||
			strings.Contains(strings.ToUpper(c.Hash), code) {
			t.Fatalf("secret en clair dans l'empreinte : %+v", c)
		}
		if len(c.Hash) != 64 {
			t.Fatalf("empreinte de %d caractères, attendu 64 (SHA-256)", len(c.Hash))
		}
	}
	if rec.puts[0].Hash != Hash(testKey, "g", KindLink, tok) || rec.puts[1].Hash != Hash(testKey, "g", KindCode, code) {
		t.Fatal("empreintes inattendues")
	}
}

type recordingStore struct {
	*MemoryStore
	puts []Credential
}

func (r *recordingStore) Put(ctx context.Context, c Credential) error {
	r.puts = append(r.puts, c)
	return r.MemoryStore.Put(ctx, c)
}

func TestRenewalInvalidatesPreviousSecret(t *testing.T) {
	st := NewMemoryStore()
	svc, _ := NewService(st, LoginCodeLength, testKey)
	ctx := context.Background()
	oldTok, _ := svc.IssueLink(ctx, "g", "p")
	oldCode, _ := svc.IssueCode(ctx, "g", "p")
	newTok, _ := svc.IssueLink(ctx, "g", "p")
	newCode, _ := svc.IssueCode(ctx, "g", "p")
	if _, err := svc.ResolveLink(ctx, "g", oldTok); err == nil {
		t.Fatal("ancien jeton encore accepté")
	}
	if _, err := svc.ResolveCode(ctx, "g", oldCode); err == nil {
		t.Fatal("ancien code encore accepté")
	}
	if p, err := svc.ResolveLink(ctx, "g", newTok); err != nil || p != "p" {
		t.Fatalf("nouveau jeton : %q, %v", p, err)
	}
	if p, err := svc.ResolveCode(ctx, "g", newCode); err != nil || p != "p" {
		t.Fatalf("nouveau code : %q, %v", p, err)
	}
	if st.Len() != 2 {
		t.Fatalf("%d accès conservés, attendu 2", st.Len())
	}
}

func TestResolveCodeToleratesFormatting(t *testing.T) {
	svc, _ := NewService(NewMemoryStore(), LoginCodeLength, testKey)
	ctx := context.Background()
	code, _ := svc.IssueCode(ctx, "g", "p")
	typed := " " + strings.ToLower(code[:4]) + "-" + code[4:] + " "
	if p, err := svc.ResolveCode(ctx, "g", typed); err != nil || p != "p" {
		t.Fatalf("ResolveCode(%q) = %q, %v", typed, p, err)
	}
}

func TestLinkAndCodeAreNotInterchangeable(t *testing.T) {
	st := NewMemoryStore()
	svc, _ := NewService(st, LoginCodeLength, testKey)
	ctx := context.Background()
	code, _ := svc.IssueCode(ctx, "g", "p")
	// Même secret, natures différentes : empreintes différentes.
	if Hash(testKey, "g", KindCode, code) == Hash(testKey, "g", KindLink, code) {
		t.Fatal("la nature n'entre pas dans l'empreinte")
	}
	if _, err := svc.ResolveLink(ctx, "g", code); err == nil {
		t.Fatal("un code accepté comme jeton")
	}
}
