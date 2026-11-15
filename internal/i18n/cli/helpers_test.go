package cli

import (
	"io"
	"log/slog"
	"testing"
	"testing/fstest"

	"secretsanta/internal/i18n/catalog"
)

func testCatalog(t *testing.T) *catalog.Catalog {
	t.Helper()
	fsys := fstest.MapFS{
		"static/i18n/core/fr.json": {Data: []byte(`{"_meta":{"code":"fr","name":"Français"},"core.hello":"Bonjour {name}","core.items.one":"{n} cadeau","core.items.other":"{n} cadeaux","core.quote":"Dit \"oui\" <b>"}`)},
		"static/i18n/core/en.json": {Data: []byte(`{"_meta":{"code":"en","name":"English"},"core.hello":"Hello {name}","core.items.one":"{n} gift","core.items.other":"{n} gifts","core.quote":"Says \"yes\" <b>"}`)},
	}
	c, err := catalog.Load(fsys, catalog.Options{Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})
	if err != nil {
		t.Fatal(err)
	}
	return c
}
