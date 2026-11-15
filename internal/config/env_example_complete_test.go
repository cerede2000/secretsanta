package config

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

var envLine = regexp.MustCompile(`^#?\s*([A-Z][A-Z0-9_]*)=`)

func exampleVars(t *testing.T) map[string]bool {
	t.Helper()
	b, err := os.ReadFile("../../.env.example")
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]bool{}
	for _, line := range strings.Split(string(b), "\n") {
		if m := envLine.FindStringSubmatch(line); m != nil {
			got[m[1]] = true
		}
	}
	return got
}

func sorted(m map[string]bool) []string {
	var s []string
	for k := range m {
		s = append(s, k)
	}
	sort.Strings(s)
	return s
}

func TestEnvExampleMatchesDeclaredVariables(t *testing.T) {
	ex := exampleVars(t)
	decl := map[string]bool{}
	for _, v := range Variables {
		decl[v] = true
		if !ex[v] {
			t.Errorf("%s est lue par le code mais absente de .env.example", v)
		}
	}
	for _, v := range sorted(ex) {
		if !decl[v] {
			t.Errorf("%s figure dans .env.example mais n'est lue par aucun code", v)
		}
	}
}

func TestRequiredVariablesAreActiveInExample(t *testing.T) {
	b, _ := os.ReadFile("../../.env.example")
	for _, v := range RequiredVariables {
		if !regexp.MustCompile(`(?m)^` + v + `=`).Match(b) {
			t.Errorf("%s est obligatoire : sa ligne ne doit pas être commentée", v)
		}
	}
}

// Toute lecture directe de l'environnement dans le dépôt doit viser une
// variable déclarée (et donc présente dans .env.example).
func TestEveryEnvReadInRepoIsDeclared(t *testing.T) {
	decl := map[string]bool{}
	for _, v := range Variables {
		decl[v] = true
	}
	root := "../.."
	for _, dir := range []string{"internal", "cmd"} {
		_ = filepath.WalkDir(filepath.Join(root, dir), func(p string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() || !strings.HasSuffix(p, ".go") || strings.HasSuffix(p, "_test.go") {
				return nil
			}
			f, perr := parser.ParseFile(token.NewFileSet(), p, nil, 0)
			if perr != nil {
				t.Errorf("%s : %v", p, perr)
				return nil
			}
			ast.Inspect(f, func(n ast.Node) bool {
				call, ok := n.(*ast.CallExpr)
				if !ok || len(call.Args) == 0 {
					return true
				}
				sel, ok := call.Fun.(*ast.SelectorExpr)
				if !ok || (sel.Sel.Name != "Getenv" && sel.Sel.Name != "LookupEnv") {
					return true
				}
				if lit, ok := call.Args[0].(*ast.BasicLit); ok {
					name := strings.Trim(lit.Value, `"`)
					if !decl[name] {
						t.Errorf("%s lit %s, variable non déclarée dans config.Variables", p, name)
					}
				}
				return true
			})
			return nil
		})
	}
}
