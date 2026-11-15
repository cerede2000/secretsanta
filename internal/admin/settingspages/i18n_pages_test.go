package settingspages

import (
	"encoding/json"
	"io/fs"
	"net/url"
	"regexp"
	"strings"
	"testing"

	"secretsanta/web"
)

func moduleCatalog(t *testing.T, lang string) map[string]string {
	t.Helper()
	b, err := fs.ReadFile(web.I18n, "static/i18n/admin/settings/"+lang+".json")
	if err != nil {
		t.Fatal(err)
	}
	m := map[string]string{}
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	return m
}

func TestSettingsCatalogsHaveSameKeysAndParams(t *testing.T) {
	fr, en := moduleCatalog(t, "fr"), moduleCatalog(t, "en")
	param := regexp.MustCompile(`\{[a-z_]+\}`)
	for k, v := range fr {
		if !strings.HasPrefix(k, "admin.settings.") {
			t.Errorf("clé %q sans préfixe admin.settings.", k)
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
	for _, k := range []string{keyTitle, keyRemindTitle, keyForbidden, keyInternal, keyInvalid, keyName, keyBudget, keyEventDate,
		keyRevealDate, keyMaxRange, keyMaxConfirm, keyLang, keyDelay, keySpacing, keyCap, keyMailTo, keyMailOff, keyMailFailed,
		keyNoPeople, keyKind, keySavedGroup, keySavedRhythm} {
		if fr[k] == "" || en[k] == "" {
			t.Errorf("clé de message %q manquante", k)
		}
	}
}

var rawKeyRe = regexp.MustCompile(`\badmin\.settings\.[a-z_]+(\.[a-z_]+)*`)

// F10c.AC7 : rendu anglais sans clé brute ni texte français, dans tous les états.
func TestEnglishRenderHasNoRawKeyNorFrench(t *testing.T) {
	e := newEnv(t)
	gid := e.group("anglais")
	ids := e.addN(gid, "Hortensia", 3)
	ck := e.adminCookie(gid)
	fr, en := moduleCatalog(t, "fr"), moduleCatalog(t, "en")
	page := "/anglais/admin/reglages"

	check := func(name, body string) {
		t.Helper()
		if m := rawKeyRe.FindString(body); m != "" {
			t.Errorf("%s : clé brute %q", name, m)
		}
		unescaped := strings.NewReplacer("&#39;", "'", "&#34;", `"`).Replace(body)
		for k, v := range fr {
			if len(v) >= 8 && en[k] != v && strings.Contains(unescaped, v) {
				t.Errorf("%s : texte français %q (clé %s) dans le rendu anglais", name, v, k)
			}
		}
		if !strings.Contains(body, `lang="en"`) {
			t.Errorf("%s : langue de la page", name)
		}
	}
	for _, p := range []string{page, page + "?ok=groupe", page + "?ok=relances", "/anglais/admin/relances"} {
		for _, lang := range []string{"en", "fr"} {
			rec := e.get(p, lang, ck)
			if rec.Code != 200 {
				t.Fatalf("%s : %d", p, rec.Code)
			}
			if lang == "en" {
				check(p, rec.Body.String())
			} else if rawKeyRe.MatchString(rec.Body.String()) {
				t.Errorf("%s (fr) : clé brute", p)
			}
		}
	}
	// États d'erreur : tous passent par le catalogue, aussi en anglais.
	badGroup := groupFields("")
	badBudget := groupFields("Nom")
	badBudget.Set("budget", "x")
	badMax := groupFields("Nom")
	badMax.Set("max_gifts", "4")
	for _, f := range []url.Values{badGroup, badBudget, badMax} {
		if rec := e.postLang(page+"/groupe", page, ck, f, "en"); rec.Code != 422 {
			t.Fatalf("erreur attendue : %d", rec.Code)
		} else {
			check("erreur groupe", rec.Body.String())
		}
	}
	if rec := e.postLang(page+"/relances", page, ck, url.Values{"first": {"0"}, "spacing": {"4"}, "cap": {"3"}}, "en"); rec.Code != 422 {
		t.Fatalf("rythme : %d", rec.Code)
	} else {
		check("erreur rythme", rec.Body.String())
	}
	if rec := e.postLang(page+"/courrier/test", page, ck, url.Values{"test_to": {"x"}}, "en"); rec.Code != 422 {
		t.Fatalf("test : %d", rec.Code)
	} else {
		check("erreur test", rec.Body.String())
	}
	if rec := e.postLang(page+"/courrier/test", page, ck, url.Values{"test_to": {"a@exemple.fr"}}, "en"); rec.Code != 200 {
		t.Fatalf("test ok : %d", rec.Code)
	} else {
		check("test envoyé", rec.Body.String())
	}
	rp := "/anglais/admin/relances"
	for _, f := range []url.Values{{"type": {"lien"}}, {"type": {"zzz"}, "person": {"1"}}, {"type": {"lien"}, "person": {itoa(ids[0]), itoa(ids[1])}}} {
		rec := e.postLang(rp, rp, ck, f, "en")
		check("relance", rec.Body.String())
	}
}
