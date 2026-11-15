package guard

import (
	"sort"
	"strings"
	"testing"
)

func TestFrenchEnglishParity(t *testing.T) {
	cats := loadCatalogs(t)
	if len(cats) == 0 {
		t.Fatal("aucun catalogue trouvé")
	}
	modules := make([]string, 0, len(cats))
	for m := range cats {
		modules = append(modules, m)
	}
	sort.Strings(modules)
	for _, m := range modules {
		fr, en := cats[m]["fr"], cats[m]["en"]
		if fr == nil {
			t.Errorf("module %s : aucun fichier fr.json", m)
			continue
		}
		if en == nil {
			t.Errorf("module %s : aucun fichier en.json", m)
			continue
		}
		for _, k := range sortedKeys(fr) {
			if _, ok := en[k]; !ok {
				t.Errorf("module %s : la clé %q existe en français mais pas en anglais", m, k)
				continue
			}
			if a, b := params(fr[k]), params(en[k]); strings.Join(a, ",") != strings.Join(b, ",") {
				t.Errorf("module %s : paramètres différents pour %q : fr %v, en %v", m, k, a, b)
			}
		}
		for _, k := range sortedKeys(en) {
			if _, ok := fr[k]; !ok {
				t.Errorf("module %s : la clé %q existe en anglais mais pas en français", m, k)
			}
		}
		for _, k := range append(sortedKeys(fr), sortedKeys(en)...) {
			if !strings.HasPrefix(k, m+".") {
				t.Errorf("module %s : la clé %q doit être préfixée par %q", m, k, m+".")
			}
		}
	}
}

func sortedKeys(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
