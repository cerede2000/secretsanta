package mailunsub

import (
	"encoding/json"
	"io/fs"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"testing"

	"secretsanta/web"
)

func moduleCatalog(t *testing.T, lang string) map[string]string {
	t.Helper()
	b, err := fs.ReadFile(web.I18n, "static/i18n/mail/unsubscribe/"+lang+".json")
	if err != nil {
		t.Fatal(err)
	}
	m := map[string]string{}
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	return m
}

func TestUnsubscribeCatalogsHaveSameKeysAndParams(t *testing.T) {
	fr, en := moduleCatalog(t, "fr"), moduleCatalog(t, "en")
	param := regexp.MustCompile(`\{[a-z_]+\}`)
	for k, v := range fr {
		if !strings.HasPrefix(k, "mail.unsub.") {
			t.Errorf("clé %q sans préfixe mail.unsub.", k)
		}
		w, ok := en[k]
		if !ok || w == "" {
			t.Errorf("clé %q absente en anglais", k)
			continue
		}
		if strings.Join(param.FindAllString(v, -1), ",") != strings.Join(param.FindAllString(w, -1), ",") {
			t.Errorf("paramètres différents pour %q", k)
		}
	}
	for k := range en {
		if _, ok := fr[k]; !ok {
			t.Errorf("clé %q absente en français", k)
		}
	}
	for _, typ := range []string{TypeRelances, TypeNouvelleIdee} {
		a, b, c, d, f := keysFor(typ)
		for _, k := range []string{a, b, c, d, f, "mail.unsub.flash.off", "mail.unsub.flash.on", "mail.unsub.essential"} {
			if fr[k] == "" || en[k] == "" {
				t.Errorf("clé %q manquante", k)
			}
		}
	}
}

var rawKeyRe = regexp.MustCompile(`\bmail\.unsub\.[a-z_]+(\.[a-z_]+)*`)

func esc(s string) string {
	return strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&#34;", "'", "&#39;").Replace(s)
}

// F6.AC34 : le rendu anglais ne contient ni clé brute ni texte français.
func TestEnglishRenderHasNoRawKeyNorFrench(t *testing.T) {
	e := newEnv(t)
	fr, en := moduleCatalog(t, "fr"), moduleCatalog(t, "en")
	for _, typ := range []string{TypeRelances, TypeNouvelleIdee} {
		path := e.link(t, typ)
		for _, step := range []struct {
			method string
			form   url.Values
		}{{http.MethodGet, nil}, {http.MethodPost, oneClick}, {http.MethodGet, nil}, {http.MethodPost, url.Values{"action": {"resubscribe"}}}} {
			for _, lang := range []string{"en", "fr"} {
				rec := e.do(step.method, path, step.form, lang)
				body := rec.Body.String()
				if rec.Code != 200 {
					t.Fatalf("%d", rec.Code)
				}
				if m := rawKeyRe.FindString(body); m != "" {
					t.Errorf("%s (%s) : clé brute %q", typ, lang, m)
				}
				if lang != "en" {
					continue
				}
				if !strings.Contains(body, `lang="en"`) {
					t.Error("lang=en attendu")
				}
				for k, v := range fr {
					if len(strings.Fields(v)) >= 3 && en[k] != v && strings.Contains(body, esc(v)) {
						t.Errorf("texte français de %s dans la page anglaise", k)
					}
				}
				for _, w := range []string{"rappels", "Rétablir", "Garder", "essentiels", "Ne plus"} {
					if strings.Contains(body, w) {
						t.Errorf("%q dans la page anglaise", w)
					}
				}
			}
		}
	}
	if rec := e.do(http.MethodGet, e.link(t, TypeRelances), nil, "fr"); !strings.Contains(rec.Body.String(), esc(fr["mail.unsub.relances.title"])) {
		t.Error("titre français absent")
	}
}
