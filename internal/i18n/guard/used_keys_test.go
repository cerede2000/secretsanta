package guard

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"secretsanta/web"
)

const keyPat = `[a-z][a-z0-9_]*(?:\.[A-Za-z0-9_]+)+`

var (
	// {{t "clé" ...}} et {{tn "clé" ...}}, y compris dans un pipeline ou une
	// parenthèse. Les clés dynamiques ({{t .Label}}) ne sont pas vérifiables.
	tmplKeyRe = regexp.MustCompile(`\b(?:t|tn)\s+"(` + keyPat + `)"`)
	// cat.T(lang, "clé", ...) et cat.TN(lang, "clé", n, ...) dans le code Go.
	goKeyRe = regexp.MustCompile(`\.TN?\(\s*[^,()"]+(?:\([^()]*\))?\s*,\s*"(` + keyPat + `)"`)
	// Champs du code qui portent une clé de texte : Label, TitleKey, MessageCode...
	goFieldKeyRe = regexp.MustCompile(`\b(?:Label|TitleKey|LabelKey|MessageKey|Code):\s*"(` + keyPat + `)"`)
)

// templateKeys renvoie clé -> premier gabarit qui l'utilise.
func templateKeys(t *testing.T) map[string]string {
	t.Helper()
	out := map[string]string{}
	for _, f := range templateFiles(t) {
		b, err := fs.ReadFile(web.Templates, f)
		if err != nil {
			t.Fatal(err)
		}
		for _, m := range actionRe.FindAllString(string(b), -1) {
			if strings.HasPrefix(m, "{{/*") {
				continue
			}
			for _, k := range tmplKeyRe.FindAllStringSubmatch(m, -1) {
				if _, ok := out[k[1]]; !ok {
					out[k[1]] = f
				}
			}
		}
	}
	return out
}

// goKeys renvoie clé -> premier fichier Go (hors tests et hors ce paquet) qui
// l'utilise.
func goKeys(t *testing.T) map[string]string {
	t.Helper()
	root := repoRoot(t)
	out := map[string]string{}
	for _, dir := range []string{"internal", "cmd"} {
		base := filepath.Join(root, dir)
		if _, err := os.Stat(base); err != nil {
			continue
		}
		err := filepath.WalkDir(base, func(p string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			rel, _ := filepath.Rel(root, p)
			rel = filepath.ToSlash(rel)
			if d.IsDir() {
				if rel == "internal/i18n/guard" {
					return filepath.SkipDir
				}
				return nil
			}
			if !strings.HasSuffix(p, ".go") || strings.HasSuffix(p, "_test.go") {
				return nil
			}
			b, err := os.ReadFile(p)
			if err != nil {
				return err
			}
			src := string(b)
			for _, re := range []*regexp.Regexp{goKeyRe, goFieldKeyRe} {
				for _, m := range re.FindAllStringSubmatch(src, -1) {
					if _, ok := out[m[1]]; !ok {
						out[m[1]] = rel
					}
				}
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	return out
}

func TestUsedKeysExist(t *testing.T) {
	cats := loadCatalogs(t)
	fr, en := cats.lang("fr"), cats.lang("en")
	tk := templateKeys(t)
	if len(tk) == 0 {
		t.Fatal("aucune clé trouvée dans les gabarits : le garde-fou ne vérifierait rien")
	}
	check := func(used map[string]string) {
		for _, k := range sortedKeysStr(used) {
			if !hasKey(fr, k) {
				t.Errorf("clé %q utilisée dans %s absente du catalogue français", k, used[k])
			}
			if !hasKey(en, k) {
				t.Errorf("clé %q utilisée dans %s absente du catalogue anglais", k, used[k])
			}
		}
	}
	check(tk)
	check(goKeys(t))
}

func TestKeyExtraction(t *testing.T) {
	src := `<p>{{t "a.b"}}</p><a title="{{t "x.y.z"}}">{{tn "c.d" .N "n" .N}}</a>{{t .Label}}{{printf "%s" "e.f"}}`
	var got []string
	for _, m := range actionRe.FindAllString(src, -1) {
		for _, k := range tmplKeyRe.FindAllStringSubmatch(m, -1) {
			got = append(got, k[1])
		}
	}
	if strings.Join(got, ",") != "a.b,x.y.z,c.d" {
		t.Errorf("clés de gabarit extraites : %v", got)
	}
	goSrc := `x := cat.T(lang, "a.b", p); y := c.TN(r.Lang(), "c.d", 2); adminnav.Item{Label: "layout.nav.draw"}; s.T(l, name)`
	var gk []string
	for _, re := range []*regexp.Regexp{goKeyRe, goFieldKeyRe} {
		for _, m := range re.FindAllStringSubmatch(goSrc, -1) {
			gk = append(gk, m[1])
		}
	}
	if strings.Join(gk, ",") != "a.b,c.d,layout.nav.draw" {
		t.Errorf("clés Go extraites : %v", gk)
	}
}

func sortedKeysStr(m map[string]string) []string {
	return sortedKeys(m)
}
