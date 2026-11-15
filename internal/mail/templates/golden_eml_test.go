package templates

import (
	"bytes"
	"flag"
	"os"
	"path/filepath"
	"testing"
)

// updateEML régénère les fichiers de référence : option explicite
// (go test ./internal/mail/templates -run Golden -update-eml).
var updateEML = flag.Bool("update-eml", false, "régénère les fichiers .eml de référence")

// F6.AC32 : un .eml brut par modèle dans testdata, identique au rendu courant
// (horloge et aléa fixes). F12 les fait noter par SpamAssassin.
func TestGoldenEML(t *testing.T) {
	cat := testCatalog(t)
	dir := "testdata"
	if *updateEML {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	for _, k := range Kinds() {
		t.Run(string(k), func(t *testing.T) {
			_, out := buildKind(t, cat, k, sampleData(k))
			file := filepath.Join(dir, string(k)+".eml")
			if *updateEML {
				if err := os.WriteFile(file, out.Raw, 0o644); err != nil {
					t.Fatal(err)
				}
				return
			}
			want, err := os.ReadFile(file)
			if err != nil {
				t.Fatalf("fichier de référence absent (%v) : régénérer avec -update-eml", err)
			}
			if !bytes.Equal(want, out.Raw) {
				t.Errorf("%s diffère du rendu courant : relire puis régénérer avec -update-eml", file)
			}
		})
	}
}

func TestGoldenDirHasNoStrayFile(t *testing.T) {
	entries, err := os.ReadDir("testdata")
	if err != nil {
		t.Skip("testdata absent")
	}
	known := map[string]bool{}
	for _, k := range Kinds() {
		known[string(k)+".eml"] = true
	}
	for _, e := range entries {
		if !known[e.Name()] {
			t.Errorf("fichier de référence orphelin : %s", e.Name())
		}
	}
}
