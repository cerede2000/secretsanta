package token

import (
	"context"
	"go/ast"
	"go/parser"
	"go/token"
	"strings"
	"testing"

	"pgregory.net/rapid"
)

func TestCodeGeneratorLengths(t *testing.T) {
	if LoginCodeLength != 8 || VerificationCodeLength != 6 {
		t.Fatalf("longueurs %d/%d, attendu 8/6", LoginCodeLength, VerificationCodeLength)
	}
	for _, n := range []int{8, 6} {
		for i := 0; i < 2000; i++ {
			code, err := GenerateCode(n)
			if err != nil {
				t.Fatal(err)
			}
			if len(code) != n {
				t.Fatalf("code %q de %d caractères, attendu %d", code, len(code), n)
			}
			for _, r := range code {
				if !strings.ContainsRune(Alphabet, r) {
					t.Fatalf("caractère %q hors alphabet dans %q", r, code)
				}
			}
			if strings.ContainsAny(code, "0O1IL") {
				t.Fatalf("caractère ambigu dans %q", code)
			}
		}
	}
}

func TestCodeGeneratorLengthProperty(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		n := rapid.SampledFrom([]int{VerificationCodeLength, LoginCodeLength}).Draw(t, "length")
		code, err := GenerateCode(n)
		if err != nil {
			t.Fatal(err)
		}
		if !ValidCode(code, n) {
			t.Fatalf("code %q invalide pour la longueur %d", code, n)
		}
	})
}

// Tous les symboles de l'alphabet finissent par sortir : le tirage couvre
// l'alphabet entier.
func TestCodeGeneratorCoversAlphabet(t *testing.T) {
	seen := map[rune]bool{}
	for i := 0; i < 500 && len(seen) < len(Alphabet); i++ {
		code, _ := GenerateCode(LoginCodeLength)
		for _, r := range code {
			seen[r] = true
		}
	}
	if len(seen) != len(Alphabet) {
		t.Fatalf("%d symboles vus sur %d", len(seen), len(Alphabet))
	}
}

func TestCodeGeneratorRejectsBadLength(t *testing.T) {
	for _, n := range []int{-1, 0, MinCodeLength - 1, MaxCodeLength + 1} {
		if _, err := GenerateCode(n); err == nil {
			t.Errorf("GenerateCode(%d) sans erreur", n)
		}
	}
	if _, err := NewService(NewMemoryStore(), 3); err == nil {
		t.Error("NewService avec une longueur de 3 accepté")
	}
	if _, err := NewService(nil, LoginCodeLength); err == nil {
		t.Error("NewService sans magasin accepté")
	}
}

func TestServiceHonoursCodeLength(t *testing.T) {
	ctx := context.Background()
	for _, n := range []int{VerificationCodeLength, LoginCodeLength} {
		svc, err := NewService(NewMemoryStore(), n)
		if err != nil {
			t.Fatal(err)
		}
		code, err := svc.IssueCode(ctx, "g", "p")
		if err != nil {
			t.Fatal(err)
		}
		if len(code) != n || !ValidCode(code, n) {
			t.Fatalf("code %q, attendu %d caractères", code, n)
		}
		if p, err := svc.ResolveCode(ctx, "g", code); err != nil || p != "p" {
			t.Fatalf("ResolveCode = %q, %v", p, err)
		}
	}
}

func TestEqualBehaviour(t *testing.T) {
	tok, _ := NewToken()
	cases := []struct {
		a, b string
		want bool
	}{
		{tok, tok, true},
		{"", "", true},
		{tok, tok[:len(tok)-1], false},
		{tok, tok + "x", false},
		{"ABCD2345", "ABCD2346", false},
		{"ABCD2345", "BBCD2345", false},
		{"", "a", false},
	}
	for _, c := range cases {
		if got := Equal(c.a, c.b); got != c.want {
			t.Errorf("Equal(%q, %q) = %v", c.a, c.b, got)
		}
	}
}

// La comparaison est à temps constant par construction : Equal réduit ses
// deux entrées à un condensé de taille fixe puis délègue à
// subtle.ConstantTimeCompare, sans comparaison par == ni bytes.Equal ni
// retour anticipé. Une mesure chronométrique serait instable en CI ; ce test
// vérifie la construction elle-même dans le source.
func TestEqualIsConstantTime(t *testing.T) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "token.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	var fn *ast.FuncDecl
	for _, d := range f.Decls {
		if fd, ok := d.(*ast.FuncDecl); ok && fd.Name.Name == "Equal" && fd.Recv == nil {
			fn = fd
		}
	}
	if fn == nil {
		t.Fatal("fonction Equal introuvable dans token.go")
	}
	var usesSubtle, usesFixedDigest bool
	returns := 0
	ast.Inspect(fn.Body, func(n ast.Node) bool {
		switch x := n.(type) {
		case *ast.SelectorExpr:
			if id, ok := x.X.(*ast.Ident); ok {
				switch {
				case id.Name == "subtle" && x.Sel.Name == "ConstantTimeCompare":
					usesSubtle = true
				case id.Name == "sha256" && x.Sel.Name == "Sum256":
					usesFixedDigest = true
				case id.Name == "bytes" || id.Name == "strings":
					t.Errorf("Equal utilise %s.%s", id.Name, x.Sel.Name)
				}
			}
		case *ast.BinaryExpr:
			if x.Op == token.EQL || x.Op == token.NEQ {
				// Seule la comparaison du résultat entier de
				// ConstantTimeCompare est admise.
				if lit, ok := x.Y.(*ast.BasicLit); !ok || lit.Value != "1" {
					t.Errorf("comparaison directe dans Equal à %s", fset.Position(x.Pos()))
				}
			}
		case *ast.IfStmt, *ast.ForStmt, *ast.RangeStmt, *ast.SwitchStmt:
			t.Errorf("branchement dans Equal à %s", fset.Position(n.Pos()))
		case *ast.ReturnStmt:
			returns++
		}
		return true
	})
	if !usesSubtle || !usesFixedDigest || returns != 1 {
		t.Fatalf("Equal doit condenser (sha256.Sum256) puis comparer avec subtle.ConstantTimeCompare en un seul retour (subtle=%v, condensé=%v, retours=%d)",
			usesSubtle, usesFixedDigest, returns)
	}
}

// Le service compare l'empreinte retrouvée avec Equal, jamais avec ==.
func TestResolveUsesEqual(t *testing.T) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "service.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	ast.Inspect(f, func(n ast.Node) bool {
		if fd, ok := n.(*ast.FuncDecl); ok && fd.Name.Name == "resolve" {
			ast.Inspect(fd.Body, func(m ast.Node) bool {
				if call, ok := m.(*ast.CallExpr); ok {
					if id, ok := call.Fun.(*ast.Ident); ok && id.Name == "Equal" {
						found = true
					}
				}
				return true
			})
		}
		return true
	})
	if !found {
		t.Fatal("resolve ne compare pas l'empreinte avec Equal")
	}
}
