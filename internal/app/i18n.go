package app

import (
	"fmt"
	"log/slog"

	"secretsanta/internal/i18n/catalog"
	"secretsanta/web"
)

// DefaultLang est la langue par défaut de l'interface.
//
// Point d'ancrage : la phase de configuration de la vague suivante ajoutera
// APP_DEFAULT_LANG et I18N_DIR à internal/config. Elles alimenteront alors
// LoadTextsWith (DefaultLang et ExternalDir) à la place de cette constante ;
// aucune variable d'environnement n'est lue ici.
const DefaultLang = "fr"

// Texts est la façade applicative du catalogue de textes
// (internal/i18n/catalog) : elle garde l'API Text(lang, key, params...).
type Texts struct {
	// Cat est le catalogue sous-jacent (T, TN, Langs, Match).
	Cat *catalog.Catalog
}

// LoadTexts charge les catalogues embarqués avec lang comme langue de repli.
func LoadTexts(lang string) (*Texts, error) {
	return LoadTextsWith(catalog.Options{DefaultLang: lang, Logger: slog.Default()})
}

// LoadTextsWith charge les catalogues embarqués avec les options données
// (dossier externe, journal).
func LoadTextsWith(opts catalog.Options) (*Texts, error) {
	cat, err := catalog.Load(web.I18n, opts)
	if err != nil {
		return nil, fmt.Errorf("textes : %w", err)
	}
	return &Texts{Cat: cat}, nil
}

// Text renvoie le texte de la clé dans la langue (repli : langue par défaut,
// puis la clé elle-même). params : paires nom, valeur remplaçant {nom}.
func (t *Texts) Text(lang, key string, params ...any) string {
	return t.Cat.T(lang, key, catalog.Pairs(params...))
}
