package catalog

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// DefaultRoot est la racine des catalogues dans le système de fichiers embarqué.
const DefaultRoot = "static/i18n"

// Options règle le chargement.
type Options struct {
	Root        string       // racine dans fsys (défaut DefaultRoot)
	DefaultLang string       // langue de repli (défaut "fr")
	ExternalDir string       // dossier I18N_DIR facultatif
	Logger      *slog.Logger // défaut slog.Default()
}

var langCode = regexp.MustCompile(`^[a-z]{2,3}(-[A-Za-z0-9]{2,8})*$`)

// ValidLangCode rapporte si le code est un code de langue acceptable.
func ValidLangCode(code string) bool { return langCode.MatchString(code) }

// Load fusionne les catalogues embarqués de fsys (<Root>/<module>/**/<langue>.json)
// puis le dossier externe. Une clé dupliquée entre deux fichiers de la même
// langue est une erreur nommant les deux fichiers ; un fichier externe invalide
// est ignoré avec un journal.
func Load(fsys fs.FS, opts Options) (*Catalog, error) {
	if opts.Root == "" {
		opts.Root = DefaultRoot
	}
	if opts.DefaultLang == "" {
		opts.DefaultLang = "fr"
	}
	if opts.Logger == nil {
		opts.Logger = slog.Default()
	}
	c := &Catalog{
		def:    opts.DefaultLang,
		msgs:   map[string]map[string]string{},
		names:  map[string]string{},
		logger: opts.Logger,
	}
	if err := c.loadEmbedded(fsys, opts.Root); err != nil {
		return nil, err
	}
	if opts.ExternalDir != "" {
		c.loadExternal(opts.ExternalDir)
	}
	if _, ok := c.msgs[c.def]; !ok {
		return nil, fmt.Errorf("langue par défaut %q : aucun catalogue chargé", c.def)
	}
	for code := range c.msgs {
		if c.names[code] == "" {
			c.names[code] = code
		}
	}
	return c, nil
}

// ParseFlat lit un fichier plat : renvoie les textes, la méta éventuelle et la
// référence éventuelle (_ref). Toute valeur non textuelle est une erreur.
func ParseFlat(data []byte) (msgs map[string]string, meta map[string]string, ref map[string]string, err error) {
	var raw map[string]json.RawMessage
	if err = json.Unmarshal(data, &raw); err != nil {
		return nil, nil, nil, fmt.Errorf("JSON invalide : %w", err)
	}
	msgs = map[string]string{}
	for k, v := range raw {
		switch k {
		case "_meta", "_ref":
			var m map[string]string
			if err = json.Unmarshal(v, &m); err != nil {
				return nil, nil, nil, fmt.Errorf("clé %s : objet de textes attendu : %w", k, err)
			}
			if k == "_meta" {
				meta = m
			} else {
				ref = m
			}
		default:
			var s string
			if err = json.Unmarshal(v, &s); err != nil {
				return nil, nil, nil, fmt.Errorf("clé %s : texte attendu : %w", k, err)
			}
			msgs[k] = s
		}
	}
	return msgs, meta, ref, nil
}

func (c *Catalog) loadEmbedded(fsys fs.FS, root string) error {
	var files []string
	err := fs.WalkDir(fsys, root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(p, ".json") {
			return nil
		}
		rel := strings.TrimPrefix(p, root+"/")
		if !strings.Contains(rel, "/") {
			return nil // un fichier doit appartenir à un module
		}
		files = append(files, p)
		return nil
	})
	if err != nil {
		return fmt.Errorf("lecture des catalogues : %w", err)
	}
	sort.Strings(files)
	origin := map[string]map[string]string{} // langue -> clé -> fichier
	var problems []error
	for _, f := range files {
		lang := strings.TrimSuffix(path.Base(f), ".json")
		data, err := fs.ReadFile(fsys, f)
		if err != nil {
			return fmt.Errorf("lecture de %s : %w", f, err)
		}
		msgs, meta, _, err := ParseFlat(data)
		if err != nil {
			return fmt.Errorf("catalogue %s : %w", f, err)
		}
		if c.msgs[lang] == nil {
			c.msgs[lang] = map[string]string{}
			origin[lang] = map[string]string{}
		}
		if name := meta["name"]; name != "" && c.names[lang] == "" {
			c.names[lang] = name
		}
		keys := make([]string, 0, len(msgs))
		for k := range msgs {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			if prev, dup := origin[lang][k]; dup {
				problems = append(problems, fmt.Errorf("clé %q en double pour la langue %q : %s et %s", k, lang, prev, f))
				continue
			}
			origin[lang][k] = f
			c.msgs[lang][k] = msgs[k]
		}
	}
	return errors.Join(problems...)
}

func (c *Catalog) loadExternal(dir string) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		c.logger.Warn("dossier de traductions illisible, ignoré", "dir", dir, "error", err)
		return
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		file := filepath.Join(dir, e.Name())
		code := strings.TrimSuffix(e.Name(), ".json")
		if !ValidLangCode(code) {
			c.logger.Warn("fichier de traductions ignoré : code de langue invalide", "file", file)
			continue
		}
		data, err := os.ReadFile(file)
		if err != nil {
			c.logger.Warn("fichier de traductions ignoré", "file", file, "error", err)
			continue
		}
		msgs, meta, _, err := ParseFlat(data)
		if err != nil {
			c.logger.Warn("fichier de traductions ignoré", "file", file, "error", err)
			continue
		}
		if c.msgs[code] == nil {
			c.msgs[code] = map[string]string{}
		}
		for k, v := range msgs {
			if v != "" { // une valeur vide = encore à traduire
				c.msgs[code][k] = v
			}
		}
		if name := meta["name"]; name != "" {
			c.names[code] = name
		}
	}
}
