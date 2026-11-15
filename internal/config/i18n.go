package config

import (
	"io"
	"log/slog"
	"os"
	"sort"
	"strings"

	"secretsanta/internal/i18n/catalog"
	"secretsanta/web"
)

// loadLang lit APP_DEFAULT_LANG et I18N_DIR. La langue par défaut doit faire
// partie des langues chargées (embarquées plus dossier externe).
func (l *loader) loadLang(c *Config) {
	c.DefaultLang = l.str(VarDefaultLang, DefaultLang)
	c.I18nDir = l.get(VarI18nDir)
	if c.I18nDir != "" {
		d, err := os.Open(c.I18nDir)
		if err == nil {
			var st os.FileInfo
			if st, err = d.Stat(); err == nil && !st.IsDir() {
				l.fail("%s doit désigner un dossier.", VarI18nDir)
				d.Close()
				return
			}
			_, err = d.Readdirnames(1)
			if err == io.EOF {
				err = nil
			}
			d.Close()
		}
		if err != nil {
			l.fail("%s : le dossier est introuvable ou illisible.", VarI18nDir)
			return
		}
	}
	quiet := slog.New(slog.NewTextHandler(io.Discard, nil))
	_, err := catalog.Load(web.I18n, catalog.Options{DefaultLang: c.DefaultLang, ExternalDir: c.I18nDir, Logger: quiet})
	if err != nil {
		var codes []string
		if probe, perr := catalog.Load(web.I18n, catalog.Options{ExternalDir: c.I18nDir, Logger: quiet}); perr == nil {
			for _, lg := range probe.Langs() {
				codes = append(codes, lg.Code)
			}
			sort.Strings(codes)
			l.fail("%s : la langue %q n'est pas chargée (langues disponibles : %s).", VarDefaultLang, c.DefaultLang, strings.Join(codes, ", "))
			return
		}
		l.fail("%s : catalogues de textes illisibles : %v", VarDefaultLang, err)
	}
}
