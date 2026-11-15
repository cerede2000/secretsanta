package app

import (
	"net/http"
	"regexp"
	"strings"
	"testing"

	"secretsanta/internal/mailsuppress"
	"secretsanta/internal/reminderrules"
)

// Les services de la vague 7 sont construits et leurs tables migrées.
func TestWave7ServicesBuilt(t *testing.T) {
	a := newApp(t)
	if a.ReminderRules == nil || a.Suppress == nil || a.MailDiag == nil {
		t.Fatalf("service manquant : rules=%v suppress=%v diag=%v", a.ReminderRules, a.Suppress, a.MailDiag)
	}
	for _, m := range []string{reminderrules.Module, mailsuppress.Module} {
		var n int
		if err := a.DB.QueryRow(`SELECT COUNT(*) FROM schema_migrations WHERE module = ?`, m).Scan(&n); err != nil || n == 0 {
			t.Errorf("module %s non appliqué (%d, %v)", m, n, err)
		}
	}
	if a.ReminderRules.Location() != a.Cfg.Location && a.Cfg.Location != nil {
		t.Error("le fuseau des règles doit être celui de la configuration")
	}
}

// La vraie page de découverte embarque les deux styles de révélation
// (enveloppe, cadeau) : scripts, feuilles et textes, en français et en
// anglais, sans script ni style en ligne (CSP stricte).
func TestRevealPageCarriesEnvelopeAndGiftModules(t *testing.T) {
	a := newApp(t)
	e := newRevealEnv(t, a)
	e.drawNow()
	b := e.login(0)
	inline := regexp.MustCompile(`(?i)<script(?: type="module")?>|\sstyle=`)
	for _, lang := range []string{"fr", "en"} {
		b.headers["Accept-Language"] = lang
		page := b.get("/" + revealSlug + "/decouverte")
		body := page.Body.String()
		if page.Code != http.StatusOK {
			t.Fatalf("%s : statut %d\n%s", lang, page.Code, body)
		}
		for _, want := range []string{
			`/static/js/reveal/core/main.js`,
			`/static/js/reveal/envelope/envelope.js`, `/static/css/reveal/envelope/envelope.css`, `data-envelope-labels`,
			`/static/js/reveal/gift/gift.js`, `/static/css/reveal/gift/gift.css`, `data-reveal-gift`,
			a.Cat.T(lang, "reveal.envelope.seal", nil), a.Cat.T(lang, "reveal.gift.hold", nil),
		} {
			if !strings.Contains(body, want) {
				t.Errorf("%s : %q absent de la page", lang, want)
			}
		}
		for _, bad := range []string{"ZgotmplZ", "reveal.envelope.", "reveal.gift.", "<no value>", "&lt;no value&gt;"} {
			if strings.Contains(body, bad) {
				t.Errorf("%s : %q dans la page", lang, bad)
			}
		}
		if inline.MatchString(body) {
			t.Errorf("%s : script ou style en ligne dans la page", lang)
		}
		if csp := page.Header().Get("Content-Security-Policy"); strings.Contains(csp, "unsafe-inline") {
			t.Errorf("%s : CSP laxiste : %s", lang, csp)
		}
	}
	if a.Cat.T("fr", "reveal.gift.hold", nil) == a.Cat.T("en", "reveal.gift.hold", nil) {
		t.Error("catalogue du cadeau identique en fr et en")
	}
}
