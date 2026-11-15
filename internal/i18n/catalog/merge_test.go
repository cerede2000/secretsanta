package catalog

import (
	"strings"
	"testing"
)

func TestMergeAllModules(t *testing.T) {
	lg, _ := testLogger()
	c, err := Load(baseFS(), Options{Logger: lg})
	if err != nil {
		t.Fatal(err)
	}
	if got := c.T("fr", "groups.name_required", nil); got != "Nom requis" {
		t.Errorf("fr groups: %q", got)
	}
	if got := c.T("en", "admin.dashboard.title", nil); got != "Dashboard" {
		t.Errorf("profondeur libre : %q", got)
	}
	langs := c.Langs()
	if len(langs) != 2 || langs[0] != (Lang{"en", "English"}) || langs[1] != (Lang{"fr", "Français"}) {
		t.Errorf("langues : %+v", langs)
	}
}

func TestDuplicateKeyRefusedNamingBothFiles(t *testing.T) {
	fsys := baseFS()
	fsys["static/i18n/groups/extra/fr.json"] = fsys["static/i18n/groups/fr.json"]
	lg, _ := testLogger()
	_, err := Load(fsys, Options{Logger: lg})
	if err == nil {
		t.Fatal("clé dupliquée acceptée")
	}
	for _, want := range []string{"groups.name_required", "static/i18n/groups/fr.json", "static/i18n/groups/extra/fr.json"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("message %q ne contient pas %q", err, want)
		}
	}
}

func TestSameKeyInDifferentLanguagesIsFine(t *testing.T) {
	lg, _ := testLogger()
	if _, err := Load(baseFS(), Options{Logger: lg}); err != nil {
		t.Fatal(err)
	}
}

func TestInvalidEmbeddedFileFailsAndMissingDefault(t *testing.T) {
	fsys := baseFS()
	fsys["static/i18n/groups/fr.json"].Data = []byte(`{"a": 3}`)
	if _, err := Load(fsys, Options{}); err == nil {
		t.Error("valeur non textuelle acceptée")
	}
	if _, err := Load(baseFS(), Options{DefaultLang: "de"}); err == nil {
		t.Error("langue par défaut absente acceptée")
	}
}
