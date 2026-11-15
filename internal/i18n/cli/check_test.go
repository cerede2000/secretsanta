package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func checkJSON(t *testing.T, content string) ([]Issue, int, string) {
	t.Helper()
	c := testCatalog(t)
	file := filepath.Join(t.TempDir(), "xx.json")
	if err := os.WriteFile(file, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	issues, err := CheckFile(c, file)
	if err != nil {
		t.Fatal(err)
	}
	var out, errb bytes.Buffer
	code := Run([]string{"check", file}, &out, &errb, c)
	return issues, code, out.String()
}

func kinds(is []Issue) map[string]int {
	m := map[string]int{}
	for _, i := range is {
		m[i.Kind]++
	}
	return m
}

func TestCheckCleanTranslationExitsZero(t *testing.T) {
	issues, code, _ := checkJSON(t, `{"core.hello":"Hallo {name}","core.items.one":"{n} Geschenk","core.items.other":"{n} Geschenke","core.quote":"Sagt ja"}`)
	if len(issues) != 0 || code != 0 {
		t.Errorf("%v code %d", issues, code)
	}
}

func TestCheckInvalidJSON(t *testing.T) {
	issues, code, _ := checkJSON(t, `{"core.hello": `)
	if kinds(issues)["json"] != 1 || code != 1 {
		t.Errorf("%v code %d", issues, code)
	}
	issues, code, _ = checkJSON(t, `{"core.hello": 4}`)
	if kinds(issues)["json"] != 1 || code != 1 {
		t.Errorf("%v code %d", issues, code)
	}
}

func TestCheckMissingAndExtraKeys(t *testing.T) {
	issues, code, out := checkJSON(t, `{"core.hello":"Hallo {name}","core.items.one":"{n} G","core.items.other":"{n} Gs","core.extra":"x"}`)
	k := kinds(issues)
	if k["manquante"] != 1 || k["en_trop"] != 1 || code != 1 {
		t.Errorf("%v code %d", issues, code)
	}
	if out == "" {
		t.Error("rien affiché")
	}
}

func TestCheckInconsistentParams(t *testing.T) {
	issues, code, _ := checkJSON(t, `{"core.hello":"Hallo {nom}","core.items.one":"{n} G","core.items.other":"Gs","core.quote":"q"}`)
	if kinds(issues)["parametres"] != 2 || code != 1 {
		t.Errorf("%v code %d", issues, code)
	}
}

func TestCheckIncompletePlural(t *testing.T) {
	issues, code, _ := checkJSON(t, `{"core.hello":"Hallo {name}","core.items.one":"{n} G","core.quote":"q","core.zz.other":"{n}"}`)
	k := kinds(issues)
	if k["pluriel"] != 2 || k["manquante"] != 1 || code != 1 {
		t.Errorf("%v code %d", issues, code)
	}
}

func TestCheckLangAndErrors(t *testing.T) {
	c := testCatalog(t)
	var out, errb bytes.Buffer
	if code := Run([]string{"check", "en"}, &out, &errb, c); code != 0 {
		t.Errorf("en cohérent : code %d %s %s", code, out.String(), errb.String())
	}
	if code := Run([]string{"check", "zz"}, &out, &errb, c); code != 1 {
		t.Errorf("langue inconnue : code %d", code)
	}
	if code := Run([]string{"check", filepath.Join(t.TempDir(), "absent.json")}, &out, &errb, c); code != 1 {
		t.Errorf("fichier absent : code %d", code)
	}
}
