package sqlstore

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"

	"secretsanta/internal/access/token"
	"secretsanta/internal/shared/errs"

	"pgregory.net/rapid"
)

func cred(group, participant string, kind token.Kind, secret string) token.Credential {
	return token.Credential{GroupID: group, ParticipantID: participant, Kind: kind, Hash: hashOf(group, kind, secret)}
}

// fataler couvre *testing.T et *rapid.T.
type fataler interface {
	Helper()
	Fatal(args ...any)
}

func mustFind(t fataler, st *Store, group string, kind token.Kind, hash string) (token.Credential, bool) {
	t.Helper()
	c, ok, err := st.Find(context.Background(), group, kind, hash)
	if err != nil {
		t.Fatal(err)
	}
	return c, ok
}

func TestPutThenFindReturnsStoredRow(t *testing.T) {
	st, _ := newStore(t)
	c := cred("g1", "alice", token.KindCode, "ABCD2345")
	if err := st.Put(context.Background(), c); err != nil {
		t.Fatal(err)
	}
	got, ok := mustFind(t, st, "g1", token.KindCode, c.Hash)
	if !ok || got != c {
		t.Fatalf("Find = %+v, %v ; attendu %+v", got, ok, c)
	}
	if _, ok := mustFind(t, st, "g1", token.KindCode, hashOf("g1", token.KindCode, "ZZZZ2222")); ok {
		t.Fatal("empreinte inconnue trouvée")
	}
}

// Émettre un nouveau secret pour un participant remplace le précédent, qui ne
// résout plus rien.
func TestPutReplacesPreviousSecret(t *testing.T) {
	st, d := newStore(t)
	ctx := context.Background()
	for _, kind := range []token.Kind{token.KindLink, token.KindCode} {
		old := cred("g1", "alice", kind, "ancien")
		cur := cred("g1", "alice", kind, "nouveau")
		if err := st.Put(ctx, old); err != nil {
			t.Fatal(err)
		}
		if err := st.Put(ctx, cur); err != nil {
			t.Fatal(err)
		}
		if _, ok := mustFind(t, st, "g1", kind, old.Hash); ok {
			t.Fatalf("%s : ancien secret encore trouvé", kind)
		}
		if got, ok := mustFind(t, st, "g1", kind, cur.Hash); !ok || got.ParticipantID != "alice" {
			t.Fatalf("%s : nouveau secret introuvable", kind)
		}
		// Reposer la même empreinte pour le même participant est sans effet.
		if err := st.Put(ctx, cur); err != nil {
			t.Fatalf("%s : réémission identique refusée : %v", kind, err)
		}
		if n := countRows(t, d, `WHERE group_id = 'g1' AND participant_id = 'alice' AND kind = ?`, string(kind)); n != 1 {
			t.Fatalf("%s : %d lignes pour alice, attendu 1", kind, n)
		}
	}
	// Remplacer le code ne touche pas au jeton, et inversement.
	if n := countRows(t, d, ``); n != 2 {
		t.Fatalf("%d lignes, attendu 2", n)
	}
}

// Une collision d'empreinte avec un autre participant renvoie ErrDuplicate et
// n'altère rien : le secret précédent du demandeur reste valable.
func TestPutDuplicateHashReturnsErrDuplicate(t *testing.T) {
	st, d := newStore(t)
	ctx := context.Background()
	alice := cred("g1", "alice", token.KindCode, "ABCD2345")
	bobOld := cred("g1", "bob", token.KindCode, "BBBB2345")
	for _, c := range []token.Credential{alice, bobOld} {
		if err := st.Put(ctx, c); err != nil {
			t.Fatal(err)
		}
	}
	clash := token.Credential{GroupID: "g1", ParticipantID: "bob", Kind: token.KindCode, Hash: alice.Hash}
	if err := st.Put(ctx, clash); !errors.Is(err, token.ErrDuplicate) {
		t.Fatalf("collision : %v, attendu ErrDuplicate", err)
	}
	if got, ok := mustFind(t, st, "g1", token.KindCode, alice.Hash); !ok || got.ParticipantID != "alice" {
		t.Fatalf("l'empreinte d'alice a changé de main : %+v", got)
	}
	if got, ok := mustFind(t, st, "g1", token.KindCode, bobOld.Hash); !ok || got.ParticipantID != "bob" {
		t.Fatal("l'ancien code de bob a été retiré malgré l'échec")
	}
	if n := countRows(t, d, ``); n != 2 {
		t.Fatalf("%d lignes, attendu 2", n)
	}
}

// Propriété : une empreinte n'est jamais trouvée hors de son groupe ni pour
// une autre nature, même quand groupes, participants et empreintes se
// recoupent.
func TestFindNeverCrossesGroupOrKindProperty(t *testing.T) {
	st, _ := newStore(t)
	ctx := context.Background()
	gen := 0
	rapid.Check(t, func(t *rapid.T) {
		gen++
		// Préfixe propre à chaque exemple : le magasin est partagé.
		p := fmt.Sprintf("r%d-", gen)
		groupA := p + rapid.StringMatching(`[a-z0-9]{1,8}`).Draw(t, "groupA")
		groupB := p + rapid.StringMatching(`[a-z0-9]{1,8}`).Filter(func(s string) bool { return p+s != groupA }).Draw(t, "groupB")
		participant := rapid.StringMatching(`[a-z0-9]{1,8}`).Draw(t, "participant")
		kind := rapid.SampledFrom([]token.Kind{token.KindLink, token.KindCode}).Draw(t, "kind")
		other := token.KindCode
		if kind == token.KindCode {
			other = token.KindLink
		}
		// La même valeur d'empreinte posée dans le groupe A uniquement.
		h := hashOf(groupA, kind, rapid.String().Draw(t, "secret"))
		if err := st.Put(ctx, token.Credential{GroupID: groupA, ParticipantID: participant, Kind: kind, Hash: h}); err != nil {
			t.Fatal(err)
		}
		if _, ok := mustFind(t, st, groupB, kind, h); ok {
			t.Fatalf("empreinte de %q trouvée dans %q", groupA, groupB)
		}
		if _, ok := mustFind(t, st, groupA, other, h); ok {
			t.Fatalf("empreinte %s trouvée comme %s", kind, other)
		}
		if got, ok := mustFind(t, st, groupA, kind, h); !ok || got.GroupID != groupA || got.Kind != kind || got.ParticipantID != participant {
			t.Fatalf("empreinte introuvable dans son groupe : %+v", got)
		}
		// La même empreinte peut appartenir à un autre participant dans un
		// autre groupe ou pour une autre nature sans collision.
		if err := st.Put(ctx, token.Credential{GroupID: groupB, ParticipantID: participant + "x", Kind: kind, Hash: h}); err != nil {
			t.Fatalf("autre groupe : %v", err)
		}
		if err := st.Put(ctx, token.Credential{GroupID: groupA, ParticipantID: participant + "x", Kind: other, Hash: h}); err != nil {
			t.Fatalf("autre nature : %v", err)
		}
		if got, _ := mustFind(t, st, groupA, kind, h); got.ParticipantID != participant {
			t.Fatalf("propriétaire changé : %+v", got)
		}
	})
}

// Propriété : le magasin SQL se comporte exactement comme le magasin de
// référence en mémoire sur toute suite d'émissions et de recherches.
func TestSQLStoreMatchesMemoryStoreProperty(t *testing.T) {
	ctx := context.Background()
	gen := 0
	st, _ := newStore(t)
	rapid.Check(t, func(t *rapid.T) {
		gen++
		ref := token.NewMemoryStore()
		groups := []string{fmt.Sprintf("r%d-g1", gen), fmt.Sprintf("r%d-g2", gen)}
		participants := []string{"a", "b", "c"}
		kinds := []token.Kind{token.KindLink, token.KindCode}
		secrets := []string{"s1", "s2", "s3"}
		for i, n := 0, rapid.IntRange(1, 40).Draw(t, "ops"); i < n; i++ {
			g := rapid.SampledFrom(groups).Draw(t, "group")
			k := rapid.SampledFrom(kinds).Draw(t, "kind")
			// Empreinte calculée pour le premier groupe : une même valeur
			// circule entre groupes et natures.
			h := hashOf(groups[0], token.KindCode, rapid.SampledFrom(secrets).Draw(t, "secret"))
			if rapid.Bool().Draw(t, "put") {
				c := token.Credential{GroupID: g, ParticipantID: rapid.SampledFrom(participants).Draw(t, "participant"), Kind: k, Hash: h}
				errSQL := st.Put(ctx, c)
				errRef := ref.Put(ctx, c)
				if errors.Is(errSQL, token.ErrDuplicate) != errors.Is(errRef, token.ErrDuplicate) || (errSQL == nil) != (errRef == nil) {
					t.Fatalf("Put(%+v) : SQL %v, référence %v", c, errSQL, errRef)
				}
			} else {
				gotSQL, okSQL, err := st.Find(ctx, g, k, h)
				if err != nil {
					t.Fatal(err)
				}
				gotRef, okRef, _ := ref.Find(ctx, g, k, h)
				if okSQL != okRef || gotSQL != gotRef {
					t.Fatalf("Find(%s, %s) : SQL %+v/%v, référence %+v/%v", g, k, gotSQL, okSQL, gotRef, okRef)
				}
			}
		}
	})
}

// Émissions simultanées : un seul secret par participant subsiste, et une
// même empreinte ne va qu'à un seul participant.
func TestConcurrentPuts(t *testing.T) {
	st, d := newStore(t)
	ctx := context.Background()
	const n = 16
	var wg sync.WaitGroup
	errsCh := make(chan error, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			errsCh <- st.Put(ctx, cred("g1", "alice", token.KindCode, fmt.Sprintf("secret-%d", i)))
		}(i)
	}
	wg.Wait()
	close(errsCh)
	for err := range errsCh {
		if err != nil {
			t.Fatalf("émission simultanée : %v", err)
		}
	}
	if c := countRows(t, d, `WHERE group_id = 'g1' AND participant_id = 'alice' AND kind = 'code'`); c != 1 {
		t.Fatalf("%d secrets pour alice, attendu 1", c)
	}

	shared := hashOf("g1", token.KindLink, "partage")
	results := make(chan error, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			results <- st.Put(ctx, token.Credential{GroupID: "g1", ParticipantID: fmt.Sprintf("p%d", i), Kind: token.KindLink, Hash: shared})
		}(i)
	}
	wg.Wait()
	close(results)
	ok := 0
	for err := range results {
		switch {
		case err == nil:
			ok++
		case errors.Is(err, token.ErrDuplicate):
		default:
			t.Fatalf("erreur inattendue : %v", err)
		}
	}
	if ok != 1 {
		t.Fatalf("%d participants ont reçu la même empreinte, attendu 1", ok)
	}
}

func TestPutRejectsIncompleteCredential(t *testing.T) {
	st, d := newStore(t)
	ctx := context.Background()
	h := hashOf("g", token.KindCode, "x")
	for _, c := range []token.Credential{
		{GroupID: "", ParticipantID: "p", Kind: token.KindCode, Hash: h},
		{GroupID: "g", ParticipantID: "", Kind: token.KindCode, Hash: h},
		{GroupID: "g", ParticipantID: "p", Kind: token.KindCode, Hash: ""},
		{GroupID: "g", ParticipantID: "p", Kind: "magic", Hash: h},
		{GroupID: "g", ParticipantID: "p", Kind: token.KindCode, Hash: "ABCD2345"},
	} {
		if err := st.Put(ctx, c); err == nil {
			t.Errorf("accès incomplet accepté : %+v", c)
		}
	}
	if n := countRows(t, d, ``); n != 0 {
		t.Fatalf("%d lignes écrites", n)
	}
}

// Le service d'accès complet sur le magasin SQL : émission, résolution,
// renouvellement, isolation entre groupes et entre natures.
func TestServiceOverSQLStore(t *testing.T) {
	st, _ := newStore(t)
	svc := newService(t, st)
	ctx := context.Background()

	tok, err := svc.IssueLink(ctx, "g1", "alice")
	if err != nil {
		t.Fatal(err)
	}
	code, err := svc.IssueCode(ctx, "g1", "alice")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.IssueLink(ctx, "g2", "alice"); err != nil {
		t.Fatal(err)
	}
	if p, err := svc.ResolveLink(ctx, "g1", tok); err != nil || p != "alice" {
		t.Fatalf("ResolveLink = %q, %v", p, err)
	}
	if p, err := svc.ResolveCode(ctx, "g1", code); err != nil || p != "alice" {
		t.Fatalf("ResolveCode = %q, %v", p, err)
	}
	for name, err := range map[string]error{
		"jeton dans g2": second(svc.ResolveLink(ctx, "g2", tok)),
		"code dans g2":  second(svc.ResolveCode(ctx, "g2", code)),
		"code en jeton": second(svc.ResolveLink(ctx, "g1", code)),
	} {
		if !errs.Is(err, errs.KindUnauthorized) {
			t.Fatalf("%s : %v", name, err)
		}
	}

	newTok, _ := svc.IssueLink(ctx, "g1", "alice")
	newCode, _ := svc.IssueCode(ctx, "g1", "alice")
	if _, err := svc.ResolveLink(ctx, "g1", tok); err == nil {
		t.Fatal("ancien jeton encore accepté")
	}
	if _, err := svc.ResolveCode(ctx, "g1", code); err == nil {
		t.Fatal("ancien code encore accepté")
	}
	if p, err := svc.ResolveLink(ctx, "g1", newTok); err != nil || p != "alice" {
		t.Fatalf("nouveau jeton : %q, %v", p, err)
	}
	if p, err := svc.ResolveCode(ctx, "g1", newCode); err != nil || p != "alice" {
		t.Fatalf("nouveau code : %q, %v", p, err)
	}
}

// Les codes d'un groupe nombreux restent uniques et chacun ne mène qu'à son
// participant.
func TestServiceCodesUniqueInGroupOverSQLStore(t *testing.T) {
	st, d := newStore(t)
	svc := newService(t, st)
	ctx := context.Background()
	codes := map[string]string{}
	for i := 0; i < 200; i++ {
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
		if got, err := svc.ResolveCode(ctx, "g1", code); err != nil || got != p {
			t.Fatalf("ResolveCode(%q) = %q, %v ; attendu %q", code, got, err, p)
		}
	}
	if n := countRows(t, d, `WHERE kind = 'code'`); n != 200 {
		t.Fatalf("%d codes, attendu 200", n)
	}
}

func second(_ string, err error) error { return err }
