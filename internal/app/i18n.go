package app

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"strings"

	"secretsanta/web"
)

// DefaultLang est la langue par défaut de l'interface.
const DefaultLang = "fr"

// Texts est un catalogue minimal : il ne charge que la langue par défaut.
// Une phase dédiée le remplacera (fusion de tous les modules, négociation de
// langue, dossier externe) en gardant l'API Text(lang, key, params...).
type Texts struct {
	byLang map[string]map[string]string
}

// LoadTexts fusionne les fichiers web/static/i18n/*/<lang>.json embarqués.
func LoadTexts(lang string) (*Texts, error) {
	files, err := fs.Glob(web.I18n, "static/i18n/*/"+lang+".json")
	if err != nil {
		return nil, err
	}
	if len(files) == 0 {
		return nil, fmt.Errorf("aucun texte pour la langue %q", lang)
	}
	m := map[string]string{}
	for _, f := range files {
		b, err := web.I18n.ReadFile(f)
		if err != nil {
			return nil, err
		}
		var raw map[string]json.RawMessage
		if err := json.Unmarshal(b, &raw); err != nil {
			return nil, fmt.Errorf("textes %s invalides : %w", f, err)
		}
		for k, v := range raw {
			if k == "_meta" {
				continue
			}
			var s string
			if err := json.Unmarshal(v, &s); err != nil {
				return nil, fmt.Errorf("textes %s, clé %s : %w", f, k, err)
			}
			m[k] = s
		}
	}
	return &Texts{byLang: map[string]map[string]string{lang: m}}, nil
}

// Text renvoie le texte de la clé dans la langue (repli : langue par défaut,
// puis la clé elle-même). params : paires nom, valeur remplaçant {nom}.
func (t *Texts) Text(lang, key string, params ...any) string {
	s, ok := t.byLang[lang][key]
	if !ok {
		s, ok = t.byLang[DefaultLang][key]
	}
	if !ok {
		return key
	}
	for i := 0; i+1 < len(params); i += 2 {
		s = strings.ReplaceAll(s, "{"+fmt.Sprint(params[i])+"}", fmt.Sprint(params[i+1]))
	}
	return s
}
