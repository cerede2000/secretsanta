// Package catalog charge et fusionne les catalogues de textes (un dossier par
// module, un fichier JSON plat par langue), lit un dossier externe facultatif
// et sert les textes avec repli, paramètres et pluriels.
package catalog

import (
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"sync"
)

// Lang décrit une langue chargée.
type Lang struct {
	Code string // ex. "fr"
	Name string // nom natif, ex. "Français"
}

// Params associe un nom de paramètre à sa valeur.
type Params map[string]any

// Pairs construit des paramètres à partir de paires nom, valeur (usage des
// fonctions de gabarit). Une paire incomplète est ignorée.
func Pairs(args ...any) Params {
	p := Params{}
	for i := 0; i+1 < len(args); i += 2 {
		p[fmt.Sprint(args[i])] = args[i+1]
	}
	return p
}

// Catalog contient les textes de toutes les langues chargées. Il est sûr pour
// un usage concurrent une fois construit.
type Catalog struct {
	def    string
	msgs   map[string]map[string]string
	names  map[string]string
	logger *slog.Logger

	warned sync.Map // signalements déjà journalisés
}

// DefaultLang renvoie la langue par défaut.
func (c *Catalog) DefaultLang() string { return c.def }

// Has rapporte si la langue est chargée.
func (c *Catalog) Has(lang string) bool { _, ok := c.msgs[lang]; return ok }

// Langs liste les langues chargées, triées par code.
func (c *Catalog) Langs() []Lang {
	out := make([]Lang, 0, len(c.msgs))
	for code := range c.msgs {
		out = append(out, Lang{Code: code, Name: c.names[code]})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Code < out[j].Code })
	return out
}

// Match renvoie la langue chargée correspondant à un code (insensible à la
// casse ; « en-US » retombe sur « en »).
func (c *Catalog) Match(tag string) (string, bool) {
	tag = strings.ToLower(strings.TrimSpace(strings.ReplaceAll(tag, "_", "-")))
	for tag != "" {
		for code := range c.msgs {
			if strings.ToLower(code) == tag {
				return code, true
			}
		}
		i := strings.LastIndex(tag, "-")
		if i < 0 {
			break
		}
		tag = tag[:i]
	}
	return "", false
}

// Messages renvoie une copie des textes plats d'une langue (nil si inconnue).
func (c *Catalog) Messages(lang string) map[string]string {
	m, ok := c.msgs[lang]
	if !ok {
		return nil
	}
	out := make(map[string]string, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

// Keys renvoie les clés d'une langue, triées.
func (c *Catalog) Keys(lang string) []string {
	m := c.msgs[lang]
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// warnOnce journalise un manque une seule fois.
func (c *Catalog) warnOnce(id, msg string, args ...any) {
	if _, loaded := c.warned.LoadOrStore(id, struct{}{}); loaded {
		return
	}
	c.logger.Warn(msg, args...)
}

// lookup cherche une clé dans la langue puis dans la langue par défaut.
func (c *Catalog) lookup(lang, key string) (string, bool) {
	if s, ok := c.msgs[lang][key]; ok {
		return s, true
	}
	if lang != c.def {
		if s, ok := c.msgs[c.def][key]; ok {
			c.warnOnce("fb|"+lang+"|"+key, "texte absent, repli sur la langue par défaut", "lang", lang, "key", key)
			return s, true
		}
	}
	c.warnOnce("miss|"+key, "texte absent de tous les catalogues", "lang", lang, "key", key)
	return "", false
}

// T renvoie le texte de la clé dans la langue : repli sur la langue par
// défaut puis sur la clé elle-même. Les paramètres remplacent {nom} ; le
// résultat est du texte brut (les gabarits échappent). Ne panique jamais.
func (c *Catalog) T(lang, key string, params Params) string {
	s, ok := c.lookup(lang, key)
	if !ok {
		return key
	}
	return c.render(lang, key, s, params)
}

// TN renvoie la forme plurielle (clé.one ou clé.other) choisie par la règle de
// la langue pour n. Le paramètre {n} vaut n sauf s'il est fourni.
func (c *Catalog) TN(lang, key string, n int, params Params) string {
	p := Params{"n": n}
	for k, v := range params {
		p[k] = v
	}
	for _, l := range []string{lang, c.def} {
		if _, ok := c.msgs[l]; !ok {
			continue
		}
		cat := PluralCategory(l, n)
		for _, k := range []string{key + "." + cat, key + ".other"} {
			if s, ok := c.msgs[l][k]; ok {
				if l != lang {
					c.warnOnce("fb|"+lang+"|"+k, "texte absent, repli sur la langue par défaut", "lang", lang, "key", k)
				}
				return c.render(lang, k, s, p)
			}
		}
	}
	return c.T(lang, key, p)
}

// render remplace {nom} en une seule passe : une valeur contenant {x} n'est
// jamais réinterprétée. Un paramètre absent laisse {nom} tel quel.
func (c *Catalog) render(lang, key, s string, params Params) string {
	if !strings.Contains(s, "{") {
		return s
	}
	var b strings.Builder
	for i := 0; i < len(s); {
		if s[i] == '{' {
			if j := strings.IndexByte(s[i:], '}'); j > 1 {
				name := s[i+1 : i+j]
				if validParam(name) {
					if v, ok := params[name]; ok {
						fmt.Fprint(&b, v)
					} else {
						c.warnOnce("param|"+key+"|"+name, "paramètre de texte absent", "lang", lang, "key", key, "param", name)
						b.WriteString(s[i : i+j+1])
					}
					i += j + 1
					continue
				}
			}
		}
		b.WriteByte(s[i])
		i++
	}
	return b.String()
}

func validParam(name string) bool {
	if name == "" {
		return false
	}
	for i, r := range name {
		switch {
		case r == '_', r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z':
		case r >= '0' && r <= '9' && i > 0:
		default:
			return false
		}
	}
	return true
}

// PluralCategory renvoie "one" ou "other" : français, 0 et 1 au singulier ;
// anglais et autres langues (tant qu'aucune règle n'est fournie), 1 seulement.
func PluralCategory(lang string, n int) string {
	if n < 0 {
		n = -n
	}
	base := strings.ToLower(lang)
	if i := strings.IndexAny(base, "-_"); i >= 0 {
		base = base[:i]
	}
	if base == "fr" {
		if n == 0 || n == 1 {
			return "one"
		}
		return "other"
	}
	if n == 1 {
		return "one"
	}
	return "other"
}
