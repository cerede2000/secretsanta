package drawengine

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// F4.AC7 / F4.AC10 : l'aléa du moteur vient de crypto/rand (via
// internal/shared/random) ; aucun fichier du paquet n'importe le générateur
// pseudo-aléatoire de la bibliothèque standard.
func TestRandomnessComesFromCryptoOnly(t *testing.T) {
	banned := map[string]bool{"math/" + "rand": true, "math/" + "rand/v2": true}
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	usesShared := false
	for _, f := range files {
		src, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		parsed, err := parser.ParseFile(token.NewFileSet(), f, src, parser.ImportsOnly)
		if err != nil {
			t.Fatal(err)
		}
		for _, imp := range parsed.Imports {
			path, _ := strconv.Unquote(imp.Path.Value)
			if banned[path] {
				t.Fatalf("%s importe %s", f, path)
			}
			if path == "secretsanta/internal/shared/random" && !strings.HasSuffix(f, "_test.go") {
				usesShared = true
			}
		}
	}
	if !usesShared {
		t.Fatal("le moteur devrait tirer son aléa de internal/shared/random (crypto/rand)")
	}
}
