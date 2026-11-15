package letterpages

import (
	"encoding/json"
	"io/fs"
	"net/http"
	"regexp"
	"strings"
	"testing"

	"secretsanta/internal/participants"
	"secretsanta/web"
)

func moduleCatalog(t *testing.T, lang string) map[string]string {
	t.Helper()
	b, err := fs.ReadFile(web.I18n, "static/i18n/letter/"+lang+".json")
	if err != nil {
		t.Fatal(err)
	}
	m := map[string]string{}
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	return m
}

func TestLetterCatalogsHaveSameKeysAndParams(t *testing.T) {
	fr, en := moduleCatalog(t, "fr"), moduleCatalog(t, "en")
	param := regexp.MustCompile(`\{[a-z_]+\}`)
	for k, v := range fr {
		if !strings.HasPrefix(k, "letter.") {
			t.Errorf("clé %q sans préfixe letter.", k)
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
}

var rawKeyRe = regexp.MustCompile(`\bletter\.[a-z_]+(\.[a-z_]+)+`)

func esc(s string) string {
	return strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&#34;", "'", "&#39;").Replace(s)
}

func TestEnglishRenderHasNoRawKeyNorFrench(t *testing.T) {
	e := newEnv(t)
	a := e.add(t, "Alice", participants.RoleNormal)
	e.add(t, "Bobby", participants.RoleNormal)
	e.add(t, "Carla", participants.RoleNormal)
	if _, err := e.letters.SetIntro(ctx, e.gid, a, "Hello"); err != nil {
		t.Fatal(err)
	}
	ps := e.revealed(t)
	var pair rawPair
	for _, p := range ps {
		if p.Receiver == a {
			pair = p
		}
	}
	fr, en := moduleCatalog(t, "fr"), moduleCatalog(t, "en")
	writer, reader := e.login(t, a), e.login(t, pair.Giver)
	type page struct {
		method, path string
		opt          reqOpt
	}
	pages := []page{
		{http.MethodGet, "/noel/lettre", reqOpt{cookie: writer}},
		{http.MethodGet, "/noel/lettre?bienvenue=1", reqOpt{cookie: writer}},
		{http.MethodGet, "/noel/lettre-recue/" + itoa(pair.ID), reqOpt{cookie: reader}},
		// Erreurs intégrées : titre vide, intro trop longue.
		{http.MethodPost, "/noel/lettre/idees", reqOpt{cookie: writer, multipart: map[string]string{"title": ""}}},
		{http.MethodPost, "/noel/lettre", reqOpt{cookie: writer, form: map[string][]string{"intro": {strings.Repeat("x", 2500)}}}},
	}
	for _, p := range pages {
		for _, lang := range []string{"fr", "en"} {
			o := p.opt
			o.lang = lang
			rec := e.do(t, p.method, p.path, o)
			body := rec.Body.String()
			if rec.Code >= 500 || rec.Code == http.StatusNotFound {
				t.Fatalf("%s : %d", p.path, rec.Code)
			}
			if m := rawKeyRe.FindString(body); m != "" {
				t.Errorf("%s (%s) : clé brute %q", p.path, lang, m)
			}
			if lang != "en" {
				continue
			}
			if !strings.Contains(body, `lang="en"`) {
				t.Errorf("%s : lang=en attendu", p.path)
			}
			for k, v := range fr {
				if len(strings.Fields(v)) >= 3 && en[k] != v && strings.Contains(body, esc(v)) {
					t.Errorf("%s : texte français de %s dans la page anglaise", p.path, k)
				}
			}
			for _, w := range []string{"Cher Père Noël", "Ton message", "Ajouter", "Envoyer", "Boîte aux lettres", "Lecture seule"} {
				if strings.Contains(body, w) {
					t.Errorf("%s : %q dans la page anglaise", p.path, w)
				}
			}
		}
	}
}
