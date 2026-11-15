package accesspages

import (
	"encoding/json"
	"io/fs"
	"net/http"
	"regexp"
	"strings"
	"testing"

	"secretsanta/web"
)

func moduleCatalog(t *testing.T, lang string) map[string]string {
	t.Helper()
	b, err := fs.ReadFile(web.I18n, "static/i18n/access/"+lang+".json")
	if err != nil {
		t.Fatal(err)
	}
	m := map[string]string{}
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	return m
}

func TestAccessCatalogsHaveSameKeysAndParams(t *testing.T) {
	fr, en := moduleCatalog(t, "fr"), moduleCatalog(t, "en")
	param := regexp.MustCompile(`\{[a-z_]+\}`)
	for k, v := range fr {
		if !strings.HasPrefix(k, "access.") {
			t.Errorf("clé %q sans préfixe access.", k)
		}
		w, ok := en[k]
		if !ok {
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
	for _, k := range []string{keyCodeInvalid, keyEmailInvalid, keyLinkInvalid, keyLimited, keyAdminInvalid, keyMagicSent, keyInternal, titleLogin, titleAdmin, titleConfirm} {
		if fr[k] == "" || en[k] == "" {
			t.Errorf("clé de message %q manquante", k)
		}
	}
}

var rawKeyRe = regexp.MustCompile(`\baccess\.[a-z_]+(\.[a-z_]+)+`)

func TestEnglishRenderHasNoRawKeyNorFrench(t *testing.T) {
	e := newEnv(t)
	e.createGroup(t, "noel")
	fr := moduleCatalog(t, "fr")
	en := moduleCatalog(t, "en")
	type page struct{ method, path, form string }
	pages := []page{
		{http.MethodGet, "/noel/connexion", ""},
		{http.MethodGet, "/noel/admin/connexion", ""},
		{http.MethodGet, "/noel/connexion/lien/abc", ""},
		{http.MethodGet, "/noel/acces/abc", ""},
		{http.MethodPost, "/noel/connexion/code", "code"},
		{http.MethodPost, "/noel/connexion/lien-magique", "email"},
		{http.MethodPost, "/noel/admin/connexion", "secret"},
		{http.MethodPost, "/noel/acces/abc", ""},
	}
	for _, p := range pages {
		for _, lang := range []string{"en", "fr"} {
			form := map[string][]string{}
			switch p.form {
			case "code":
				form["code"] = []string{"ZZZZZZZZ"}
			case "email":
				form["email"] = []string{"a@exemple.fr"}
			case "secret":
				form["secret"] = []string{"faux"}
			}
			rec := e.do(t, p.method, p.path, form, "198.51.100.50", lang)
			body := rec.Body.String()
			if rec.Code >= 500 {
				t.Fatalf("%s : %d", p.path, rec.Code)
			}
			if m := rawKeyRe.FindString(body); m != "" {
				t.Errorf("%s (%s) : clé brute %q", p.path, lang, m)
			}
			if lang == "en" {
				if !strings.Contains(body, `lang="en"`) {
					t.Errorf("%s : lang=en attendu", p.path)
				}
				for k, v := range fr {
					if len(strings.Fields(v)) >= 3 && en[k] != v && strings.Contains(body, htmlEscape(v)) {
						t.Errorf("%s : texte français de %s dans la page anglaise", p.path, k)
					}
				}
				for _, w := range []string{"Ouvre ta lettre", "Mot de passe", "Entrer", "Connexion", "Retour"} {
					if strings.Contains(body, w) {
						t.Errorf("%s : %q dans la page anglaise", p.path, w)
					}
				}
			}
		}
	}
}

func htmlEscape(s string) string {
	return strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&#34;", "'", "&#39;").Replace(s)
}
