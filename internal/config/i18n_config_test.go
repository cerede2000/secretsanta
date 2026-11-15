package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDefaultLangDefaultsToFrench(t *testing.T) {
	c, err := Load(env(base()))
	if err != nil {
		t.Fatal(err)
	}
	if c.DefaultLang != "fr" || c.I18nDir != "" {
		t.Fatalf("défauts : %q %q", c.DefaultLang, c.I18nDir)
	}
	c, err = Load(env(with(VarDefaultLang, "en")))
	if err != nil || c.DefaultLang != "en" {
		t.Fatalf("en refusée : %v", err)
	}
}

func TestDefaultLangRefusesUnloaded(t *testing.T) {
	mustFail(t, with(VarDefaultLang, "xx"), VarDefaultLang)
}

func TestExternalLangMakesLanguageLoaded(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "es.json"), []byte(`{"_meta":{"code":"es","name":"Español"},"core.http.method_not_allowed":"Método no permitido."}`), 0o600); err != nil {
		t.Fatal(err)
	}
	c, err := Load(env(with(VarDefaultLang, "es", VarI18nDir, dir)))
	if err != nil || c.DefaultLang != "es" || c.I18nDir != dir {
		t.Fatalf("langue externe refusée : %v", err)
	}
}

func TestI18nDirMustBeReadable(t *testing.T) {
	mustFail(t, with(VarI18nDir, filepath.Join(t.TempDir(), "absent")), VarI18nDir)
	f := filepath.Join(t.TempDir(), "fichier")
	if err := os.WriteFile(f, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	mustFail(t, with(VarI18nDir, f), VarI18nDir)
	if _, err := Load(env(with(VarI18nDir, t.TempDir()))); err != nil {
		t.Fatalf("dossier vide valide refusé : %v", err)
	}
}

func TestI18nVarsInEnvExample(t *testing.T) {
	b, err := os.ReadFile("../../.env.example")
	if err != nil {
		t.Fatal(err)
	}
	for _, v := range []string{VarDefaultLang, VarI18nDir} {
		if !strings.Contains(string(b), v+"=") {
			t.Errorf("%s absente de .env.example", v)
		}
	}
}
