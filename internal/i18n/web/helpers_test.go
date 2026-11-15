package web

import (
	"html/template"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"testing/fstest"

	"secretsanta/internal/i18n/catalog"
	"secretsanta/internal/i18n/negotiate"
	"secretsanta/internal/websec"
	embedded "secretsanta/web"
)

func testCatalog(t *testing.T) *catalog.Catalog {
	t.Helper()
	fsys := fstest.MapFS{
		"static/i18n/core/fr.json": {Data: []byte(`{"_meta":{"code":"fr","name":"Français"},"core.hello":"Bonjour {name}","core.items.one":"{n} cadeau","core.items.other":"{n} cadeaux"}`)},
		"static/i18n/core/en.json": {Data: []byte(`{"_meta":{"code":"en","name":"English"},"core.hello":"Hello {name}","core.items.one":"{n} gift","core.items.other":"{n} gifts"}`)},
		"static/i18n/i18n/fr.json": {Data: []byte(`{"i18n.selector.label":"Langue","i18n.selector.apply":"Changer"}`)},
		"static/i18n/i18n/en.json": {Data: []byte(`{"i18n.selector.label":"Language","i18n.selector.apply":"Switch"}`)},
		"static/i18n/core/de.json": {Data: []byte(`{"_meta":{"code":"de","name":"Deutsch"},"core.hello":"Hallo {name}"}`)},
	}
	cat, err := catalog.Load(fsys, catalog.Options{DefaultLang: "fr", Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})
	if err != nil {
		t.Fatal(err)
	}
	return cat
}

// reqLang fabrique une requête dont la langue est déjà négociée.
func reqLang(method, target, lang string) *http.Request {
	r := httptest.NewRequest(method, target, nil)
	return r.WithContext(negotiate.WithLang(r.Context(), lang))
}

// pageTemplate analyse les gabarits du pied de page et du sélecteur dans
// l'ordre attendu de l'orchestrateur : layout d'abord, i18n ensuite.
func pageTemplate(t *testing.T) *template.Template {
	t.Helper()
	tm := template.New("root").Funcs(websec.TemplateFuncsPlaceholder()).Funcs(FuncsPlaceholder())
	tm, err := tm.ParseFS(embedded.Templates, "templates/layout/*.html", "templates/i18n/*.html")
	if err != nil {
		t.Fatal(err)
	}
	return tm
}
