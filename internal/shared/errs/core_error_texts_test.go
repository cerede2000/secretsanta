package errs

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

func TestEveryKindHasCoreTexts(t *testing.T) {
	for _, lang := range []string{"fr", "en"} {
		b, err := os.ReadFile("../../../web/static/i18n/core/" + lang + ".json")
		if err != nil {
			t.Fatal(err)
		}
		var m map[string]any
		if err := json.Unmarshal(b, &m); err != nil {
			t.Fatal(err)
		}
		for _, k := range Kinds {
			v, ok := m[KindKey(k)].(string)
			if !ok || strings.TrimSpace(v) == "" {
				t.Errorf("%s : clé %s absente ou vide", lang, KindKey(k))
			}
		}
	}
	if KindKey(KindNotFound) != "core.errors.not_found" {
		t.Fatal("format de la clé")
	}
}
