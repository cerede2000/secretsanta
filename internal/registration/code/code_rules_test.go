package code

import (
	"context"
	"go/ast"
	"go/parser"
	"go/token"
	"strconv"
	"strings"
	"testing"
	"time"

	acctoken "secretsanta/internal/access/token"
)

// F11.AC10 : le code fait 6 caractères de l'alphabet sans caractères ambigus
// de F2, est stocké haché, expire après 15 minutes, bloque après 5 tentatives
// et se compare à temps constant.

func TestCodeIsSixCharsFromUnambiguousAlphabet(t *testing.T) {
	if Length != 6 {
		t.Fatalf("longueur %d, 6 attendu", Length)
	}
	f := newFixture(t)
	seen := map[rune]bool{}
	for reg := int64(1); reg <= 300; reg++ {
		m := f.issue(t, testGroup, reg, "p"+strconv.FormatInt(reg, 10)+"@example.org")
		if len(m.Code) != 6 {
			t.Fatalf("code %q : %d caractères", m.Code, len(m.Code))
		}
		if !acctoken.ValidCode(m.Code, 6) {
			t.Fatalf("code %q hors de l'alphabet de F2", m.Code)
		}
		if strings.ContainsAny(m.Code, "01OIL") {
			t.Fatalf("code %q avec un caractère ambigu", m.Code)
		}
		for _, r := range m.Code {
			seen[r] = true
		}
	}
	// 1800 tirages : chaque symbole de l'alphabet doit être apparu.
	for _, r := range acctoken.Alphabet {
		if !seen[r] {
			t.Errorf("symbole %q jamais tiré", r)
		}
	}
}

func TestCodeAndTokenStoredHashedOnly(t *testing.T) {
	f := newFixture(t)
	m := f.issue(t, testGroup, 1, "lea@example.org")
	tok := tokenOf(t, m)
	recs := f.store.Records()
	if len(recs) != 1 {
		t.Fatalf("%d enregistrements", len(recs))
	}
	r := recs[0]
	for _, field := range []string{r.CodeHash, r.TokenHash} {
		if len(field) != 64 {
			t.Errorf("empreinte %q : 64 caractères hexadécimaux attendus", field)
		}
		if strings.Contains(field, m.Code) || strings.Contains(field, tok) {
			t.Errorf("secret en clair dans %q", field)
		}
	}
	// Empreinte à clé : une autre clé donne une autre empreinte.
	if hashCode([]byte("autre-clé"), testGroup, 1, m.Code) == r.CodeHash {
		t.Error("l'empreinte du code ne dépend pas de la clé")
	}
	if hashCode(testKey, testGroup, 1, m.Code) != r.CodeHash {
		t.Error("empreinte du code inattendue")
	}
	if hashConfirm(testKey, testGroup, tok) != r.TokenHash {
		t.Error("empreinte du jeton inattendue")
	}
	if r.CodeHash == r.TokenHash {
		t.Error("code et jeton partagent une empreinte")
	}
}

func TestCodeExpiresAfter15Minutes(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	a := f.issue(t, testGroup, 1, "a@example.org")
	b := f.issue(t, testGroup, 2, "b@example.org")
	f.clk.Advance(15*time.Minute - time.Second)
	if _, err := f.svc.Verify(ctx, testGroup, 1, a.Code); err != nil {
		t.Fatalf("à 14 min 59 s le code doit fonctionner : %v", err)
	}
	f.clk.Advance(time.Second)
	if _, err := f.svc.Verify(ctx, testGroup, 2, b.Code); !IsRefused(err) {
		t.Fatalf("à 15 min le code doit être refusé : %v", err)
	}
}

func TestCodeBlocksAfterFiveAttempts(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)

	// 4 erreurs puis le bon code : la 5e tentative passe.
	a := f.issue(t, testGroup, 1, "a@example.org")
	for i := 0; i < 4; i++ {
		if _, err := f.svc.Verify(ctx, testGroup, 1, wrongCode(a.Code)); !IsRefused(err) {
			t.Fatalf("code faux accepté : %v", err)
		}
	}
	res, err := f.svc.Verify(ctx, testGroup, 1, a.Code)
	if err != nil {
		t.Fatalf("5e tentative avec le bon code : %v", err)
	}
	if res.RegistrationID != 1 || res.GroupID != testGroup || !res.VerifiedAt.Equal(f.clk.Now()) {
		t.Fatalf("résultat %+v", res)
	}

	// 5 erreurs : le bon code est ensuite refusé, de même que le lien.
	b := f.issue(t, testGroup, 2, "b@example.org")
	for i := 0; i < 5; i++ {
		_, _ = f.svc.Verify(ctx, testGroup, 2, wrongCode(b.Code))
	}
	if _, err := f.svc.Verify(ctx, testGroup, 2, b.Code); !IsRefused(err) {
		t.Fatalf("bon code accepté après 5 erreurs : %v", err)
	}
	if _, err := f.svc.Confirm(ctx, testGroup, tokenOf(t, b)); !IsRefused(err) {
		t.Fatalf("lien accepté après 5 erreurs : %v", err)
	}
}

func TestCodeInputNormalised(t *testing.T) {
	f := newFixture(t)
	m := f.issue(t, testGroup, 1, "a@example.org")
	in := strings.ToLower(m.Code[:3]) + " - " + strings.ToLower(m.Code[3:])
	if _, err := f.svc.Verify(context.Background(), testGroup, 1, in); err != nil {
		t.Fatalf("saisie %q refusée : %v", in, err)
	}
}

func TestCodeBoundToRegistrationAndGroup(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	a := f.issue(t, testGroup, 1, "a@example.org")
	f.issue(t, testGroup, 2, "b@example.org")
	f.issue(t, testGroup+1, 1, "c@example.org")
	if _, err := f.svc.Verify(ctx, testGroup, 2, a.Code); !IsRefused(err) {
		t.Fatal("le code d'une inscription en valide une autre")
	}
	if _, err := f.svc.Verify(ctx, testGroup+1, 1, a.Code); !IsRefused(err) {
		t.Fatal("le code d'un groupe sert dans un autre")
	}
	if _, err := f.svc.Confirm(ctx, testGroup+1, tokenOf(t, a)); !IsRefused(err) {
		t.Fatal("le lien d'un groupe sert dans un autre")
	}
	if _, err := f.svc.Verify(ctx, testGroup, 1, a.Code); err != nil {
		t.Fatalf("le bon code doit encore fonctionner : %v", err)
	}
}

// La comparaison des empreintes passe par token.Equal (temps constant) et
// jamais par == ou != sur une empreinte, ni par bytes.Equal ou strings.
func TestHashesComparedInConstantTime(t *testing.T) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "service.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	equalCalls := 0
	isHash := func(e ast.Expr) bool {
		switch x := e.(type) {
		case *ast.SelectorExpr:
			return strings.HasSuffix(x.Sel.Name, "Hash")
		case *ast.Ident:
			return x.Name == "h" || x.Name == "th" || strings.HasSuffix(x.Name, "Hash")
		}
		return false
	}
	ast.Inspect(file, func(n ast.Node) bool {
		switch x := n.(type) {
		case *ast.BinaryExpr:
			if (x.Op == token.EQL || x.Op == token.NEQ) && (isHash(x.X) || isHash(x.Y)) {
				t.Errorf("%s : empreinte comparée avec %s", fset.Position(x.Pos()), x.Op)
			}
		case *ast.CallExpr:
			if sel, ok := x.Fun.(*ast.SelectorExpr); ok {
				if id, ok := sel.X.(*ast.Ident); ok {
					if id.Name == "token" && sel.Sel.Name == "Equal" {
						equalCalls++
					}
					if (id.Name == "bytes" || id.Name == "strings") && (sel.Sel.Name == "Equal" || sel.Sel.Name == "EqualFold" || sel.Sel.Name == "Compare") {
						t.Errorf("%s : comparaison non constante %s.%s", fset.Position(x.Pos()), id.Name, sel.Sel.Name)
					}
				}
			}
		}
		return true
	})
	// Verify (code), lookupToken (jeton et code dérivé).
	if equalCalls < 3 {
		t.Fatalf("%d appels à token.Equal, au moins 3 attendus", equalCalls)
	}
	if !acctoken.Equal("abc", "abc") || acctoken.Equal("abc", "abd") {
		t.Fatal("token.Equal")
	}
}
