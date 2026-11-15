package cli

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
)

func TestExportListsAllDefaultKeysWithRef(t *testing.T) {
	c := testCatalog(t)
	var buf bytes.Buffer
	if err := Export(&buf, c, "de"); err != nil {
		t.Fatal(err)
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(buf.Bytes(), &m); err != nil {
		t.Fatalf("export non JSON : %v\n%s", err, buf.String())
	}
	var ref map[string]string
	if err := json.Unmarshal(m["_ref"], &ref); err != nil {
		t.Fatal(err)
	}
	for k, v := range c.Messages("fr") {
		if ref[k] != v {
			t.Errorf("_ref[%s]=%q", k, ref[k])
		}
		var got string
		if err := json.Unmarshal(m[k], &got); err != nil || got != v {
			t.Errorf("valeur %s=%q", k, got)
		}
	}
	if !strings.Contains(buf.String(), `"code":"de"`) {
		t.Error("méta absente")
	}
	if strings.Contains(buf.String(), "u003c") {
		t.Error("HTML échappé inutilement")
	}
}

func TestExportThenCheckReportsOnlyValuesToTranslate(t *testing.T) {
	c := testCatalog(t)
	file := filepath.Join(t.TempDir(), "de.json")
	var out, errb bytes.Buffer
	if code := Run([]string{"export", "-o", file, "de"}, &out, &errb, c); code != 0 {
		t.Fatalf("export code %d : %s", code, errb.String())
	}
	issues, err := CheckFile(c, file)
	if err != nil {
		t.Fatal(err)
	}
	if len(issues) != 4 {
		t.Fatalf("écarts : %v", issues)
	}
	for _, is := range issues {
		if is.Kind != "a_traduire" {
			t.Errorf("écart autre qu'à traduire : %v", is)
		}
	}
	out.Reset()
	errb.Reset()
	if code := Run([]string{"check", file}, &out, &errb, c); code != 1 {
		t.Errorf("code %d", code)
	}
}

func TestExportExistingLanguageKeepsTranslations(t *testing.T) {
	c := testCatalog(t)
	var buf bytes.Buffer
	if err := Export(&buf, c, "en"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), `"core.hello": "Hello {name}"`) || !strings.Contains(buf.String(), `"name":"English"`) {
		t.Errorf("%s", buf.String())
	}
}

func TestExportRefusesBadCodeAndUsage(t *testing.T) {
	c := testCatalog(t)
	var buf bytes.Buffer
	if err := Export(&buf, c, "../x"); err == nil {
		t.Error("code invalide accepté")
	}
	var o, e bytes.Buffer
	if Run([]string{"export"}, &o, &e, c) != 2 || Run(nil, &o, &e, c) != 2 || Run([]string{"zzz"}, &o, &e, c) != 2 {
		t.Error("usage incorrect : code 2 attendu")
	}
}
