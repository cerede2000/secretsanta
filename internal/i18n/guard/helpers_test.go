package guard

import (
	"encoding/json"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"testing"

	"secretsanta/web"
)

// catalogs : module -> langue -> clés plates (tous fichiers du module fusionnés).
type catalogs map[string]map[string]map[string]string

// loadCatalogs lit web/static/i18n/<module>/**/<langue>.json depuis le système
// de fichiers embarqué. Une clé dupliquée dans deux fichiers du même module et
// de la même langue est une erreur (le paquet catalog la refuse aussi).
func loadCatalogs(t *testing.T) catalogs {
	t.Helper()
	out := catalogs{}
	err := fs.WalkDir(web.I18n, "static/i18n", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(p, ".json") {
			return nil
		}
		rel := strings.TrimPrefix(p, "static/i18n/")
		parts := strings.Split(rel, "/")
		if len(parts) < 2 {
			t.Errorf("%s : un catalogue doit vivre dans un dossier de module", p)
			return nil
		}
		module := parts[0]
		lang := strings.TrimSuffix(parts[len(parts)-1], ".json")
		raw, err := fs.ReadFile(web.I18n, p)
		if err != nil {
			return err
		}
		var flat map[string]json.RawMessage
		if err := json.Unmarshal(raw, &flat); err != nil {
			t.Errorf("%s : JSON invalide : %v", p, err)
			return nil
		}
		if out[module] == nil {
			out[module] = map[string]map[string]string{}
		}
		if out[module][lang] == nil {
			out[module][lang] = map[string]string{}
		}
		for k, v := range flat {
			if k == "_meta" {
				continue
			}
			var s string
			if err := json.Unmarshal(v, &s); err != nil {
				t.Errorf("%s : la valeur de %q n'est pas une chaîne", p, k)
				continue
			}
			if _, dup := out[module][lang][k]; dup {
				t.Errorf("%s : clé %q déjà définie dans un autre fichier du module %s (%s)", p, k, module, lang)
			}
			out[module][lang][k] = s
		}
		return nil
	})
	if err != nil {
		t.Fatalf("lecture des catalogues : %v", err)
	}
	return out
}

// lang renvoie l'union des clés d'une langue sur tous les modules.
func (c catalogs) lang(code string) map[string]string {
	all := map[string]string{}
	for _, langs := range c {
		for k, v := range langs[code] {
			all[k] = v
		}
	}
	return all
}

var paramRe = regexp.MustCompile(`\{([A-Za-z_][A-Za-z0-9_]*)\}`)

// params renvoie la liste triée des paramètres {x} d'une valeur.
func params(s string) []string {
	seen := map[string]bool{}
	for _, m := range paramRe.FindAllStringSubmatch(s, -1) {
		seen[m[1]] = true
	}
	out := make([]string, 0, len(seen))
	for k := range seen {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// hasKey rapporte si une clé existe, telle quelle ou en forme plurielle
// (clé.one / clé.other, utilisée par tn).
func hasKey(msgs map[string]string, key string) bool {
	if _, ok := msgs[key]; ok {
		return true
	}
	_, ok := msgs[key+".other"]
	return ok
}

// repoRoot remonte jusqu'au dossier qui contient go.mod.
func repoRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller")
	}
	dir := filepath.Dir(file)
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("go.mod introuvable")
		}
		dir = parent
	}
}

// templateFiles liste les gabarits embarqués (chemin relatif à web/).
func templateFiles(t *testing.T) []string {
	t.Helper()
	var files []string
	err := fs.WalkDir(web.Templates, "templates", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() && path.Ext(p) == ".html" || !d.IsDir() && path.Ext(p) == ".txt" {
			files = append(files, p)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return files
}
