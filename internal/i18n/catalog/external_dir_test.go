package catalog

import (
	"os"
	"path/filepath"
	"testing"
)

func write(t *testing.T, dir, name, data string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestExternalDirAddsLanguageAndOverrides(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "de.json", `{"_meta":{"code":"de","name":"Deutsch"},"core.hello":"Hallo {name}"}`)
	write(t, dir, "en.json", `{"core.hello":"Hi {name}","core.empty":""}`)
	lg, _ := testLogger()
	c, err := Load(baseFS(), Options{ExternalDir: dir, Logger: lg})
	if err != nil {
		t.Fatal(err)
	}
	if got := c.T("de", "core.hello", Params{"name": "A"}); got != "Hallo A" {
		t.Errorf("langue ajoutée : %q", got)
	}
	if got := c.T("de", "groups.name_required", nil); got != "Nom requis" {
		t.Errorf("repli de la langue ajoutée : %q", got)
	}
	if got := c.T("en", "core.hello", Params{"name": "A"}); got != "Hi A" {
		t.Errorf("surcharge : %q", got)
	}
	if got := c.T("en", "groups.name_required", nil); got != "Name required" {
		t.Errorf("clé non surchargée : %q", got)
	}
	var found bool
	for _, l := range c.Langs() {
		if l == (Lang{"de", "Deutsch"}) {
			found = true
		}
	}
	if !found {
		t.Errorf("de absente : %+v", c.Langs())
	}
}

func TestExternalInvalidFileIgnoredWithLog(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "es.json", `{ceci n'est pas du json`)
	write(t, dir, "it.json", `{"core.hello": 12}`)
	write(t, dir, "Bad Name.json", `{}`)
	write(t, dir, "pt.json", `{"core.hello":"Olá {name}"}`)
	lg, buf := testLogger()
	c, err := Load(baseFS(), Options{ExternalDir: dir, Logger: lg})
	if err != nil {
		t.Fatalf("un fichier invalide ne doit pas empêcher le démarrage : %v", err)
	}
	if c.Has("es") || c.Has("it") {
		t.Error("langue invalide chargée")
	}
	if !c.Has("pt") {
		t.Error("fichier valide voisin ignoré")
	}
	if buf.Count("fichier de traductions ignoré") != 3 {
		t.Errorf("journal : %s", buf.String())
	}
}

func TestExternalDirMissingDoesNotFail(t *testing.T) {
	lg, buf := testLogger()
	if _, err := Load(baseFS(), Options{ExternalDir: filepath.Join(t.TempDir(), "absent"), Logger: lg}); err != nil {
		t.Fatal(err)
	}
	if buf.Count("illisible") != 1 {
		t.Errorf("journal : %s", buf.String())
	}
}
