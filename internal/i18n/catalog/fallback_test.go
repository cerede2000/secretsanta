package catalog

import (
	"sync"
	"testing"
)

func TestFallbackChainAndLogOnce(t *testing.T) {
	lg, buf := testLogger()
	c, err := Load(baseFS(), Options{Logger: lg})
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 5; i++ {
		if got := c.T("en", "core.only_fr", nil); got != "Seulement" {
			t.Fatalf("repli par défaut : %q", got)
		}
		if got := c.T("en", "nope.key", nil); got != "nope.key" {
			t.Fatalf("repli clé : %q", got)
		}
	}
	if n := buf.Count("repli sur la langue par défaut"); n != 1 {
		t.Errorf("repli journalisé %d fois", n)
	}
	if n := buf.Count("absent de tous les catalogues"); n != 1 {
		t.Errorf("manque journalisé %d fois", n)
	}
}

func TestUnknownLanguageFallsBack(t *testing.T) {
	lg, _ := testLogger()
	c, _ := Load(baseFS(), Options{Logger: lg})
	if got := c.T("zz", "groups.name_required", nil); got != "Nom requis" {
		t.Errorf("%q", got)
	}
	if got := c.TN("zz", "x.items", 2, nil); got != "x.items" {
		t.Errorf("%q", got)
	}
}

func TestNoPanicOnMissingKeyOrParam(t *testing.T) {
	lg, _ := testLogger()
	c, _ := Load(baseFS(), Options{Logger: lg})
	if got := c.T("fr", "core.hello", nil); got != "Bonjour {name}" {
		t.Errorf("paramètre absent : %q", got)
	}
	if got := c.T("fr", "", Params{"a": nil}); got != "" {
		t.Errorf("clé vide : %q", got)
	}
	if got := Pairs("a"); len(got) != 0 {
		t.Errorf("paire incomplète : %v", got)
	}
}

func TestConcurrentUse(t *testing.T) {
	lg, buf := testLogger()
	c, _ := Load(baseFS(), Options{Logger: lg})
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); c.T("en", "core.only_fr", nil); c.T("en", "zzz", nil) }()
	}
	wg.Wait()
	if buf.Count("repli sur la langue par défaut") != 1 {
		t.Errorf("journal : %s", buf.String())
	}
}
