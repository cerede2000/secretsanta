// Package web fournit les fonctions de gabarit t et tn, les fonctions du
// sélecteur de langue et sa route de changement.
package web

import (
	"fmt"
	"html/template"
	"log/slog"
	"net/http"
	"strconv"

	"secretsanta/internal/i18n/catalog"
	"secretsanta/internal/i18n/negotiate"
)

// Translator est ce que ce paquet demande au catalogue (*catalog.Catalog).
type Translator interface {
	T(lang, key string, params catalog.Params) string
	TN(lang, key string, n int, params catalog.Params) string
	Langs() []catalog.Lang
	Match(tag string) (string, bool)
	DefaultLang() string
}

// FuncsPlaceholder déclare les fonctions au moment de l'analyse des gabarits ;
// elles rendent la clé telle quelle tant que Funcs(...) n'est pas appliqué.
func FuncsPlaceholder() template.FuncMap {
	return template.FuncMap{
		"t":           func(key string, _ ...any) string { return key },
		"tn":          func(key string, _ any, _ ...any) string { return key },
		"langs":       func() []catalog.Lang { return nil },
		"langCurrent": func() string { return "" },
		"langNext":    func() string { return "/" },
	}
}

// Funcs renvoie les fonctions liées à la requête (à appliquer sur un clone du
// gabarit, comme websec.TemplateFuncs) :
//
//	t "clé" "nom" valeur ...        texte de la langue de la requête
//	tn "clé" n "nom" valeur ...     pluriel (clé.one / clé.other), {n} = n
//	langs                           langues chargées (Code, Name natif)
//	langCurrent                     langue de la requête
//	langNext                        chemin local où revenir après un changement
//
// Les valeurs ne sont jamais du HTML : html/template échappe le résultat.
// Aucune de ces fonctions ne renvoie d'erreur ni ne panique.
func Funcs(tr Translator, r *http.Request) template.FuncMap {
	lang := negotiate.Lang(r.Context(), tr.DefaultLang())
	return template.FuncMap{
		"t": func(key string, args ...any) (out string) {
			defer func() {
				if rec := recover(); rec != nil {
					slog.Warn("texte : panique évitée", "key", key, "err", fmt.Sprint(rec))
					out = key
				}
			}()
			return tr.T(lang, key, catalog.Pairs(args...))
		},
		"tn": func(key string, n any, args ...any) (out string) {
			defer func() {
				if rec := recover(); rec != nil {
					slog.Warn("texte : panique évitée", "key", key, "err", fmt.Sprint(rec))
					out = key
				}
			}()
			return tr.TN(lang, key, toInt(n), catalog.Pairs(args...))
		},
		"langs":       func() []catalog.Lang { return tr.Langs() },
		"langCurrent": func() string { return lang },
		"langNext":    func() string { return SafeNext(r.URL.RequestURI()) },
	}
}

func toInt(n any) int {
	switch v := n.(type) {
	case int:
		return v
	case int8:
		return int(v)
	case int16:
		return int(v)
	case int32:
		return int(v)
	case int64:
		return int(v)
	case uint:
		return int(v)
	case uint8:
		return int(v)
	case uint16:
		return int(v)
	case uint32:
		return int(v)
	case uint64:
		return int(v)
	case float32:
		return int(v)
	case float64:
		return int(v)
	case string:
		i, _ := strconv.Atoi(v)
		return i
	}
	return 0
}
