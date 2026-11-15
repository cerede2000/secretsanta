package main

import (
	"bytes"
	"testing"
)

// La sous-commande i18n s'exécute sans aucun secret d'exécution.
func TestRunI18nCheckEmbedded(t *testing.T) {
	for _, lang := range []string{"fr", "en"} {
		var out, errb bytes.Buffer
		if code := runI18n([]string{"check", lang}, &out, &errb); code != 0 {
			t.Errorf("i18n check %s : code %d\n%s%s", lang, code, out.String(), errb.String())
		}
	}
	var out, errb bytes.Buffer
	if code := runI18n(nil, &out, &errb); code == 0 {
		t.Error("sans argument : code non nul attendu")
	}
}
