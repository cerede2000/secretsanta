package drawpages

import (
	"encoding/json"
	"io/fs"
	"regexp"
	"strings"
	"testing"

	"secretsanta/internal/participants"
	"secretsanta/web"
)

func moduleCatalog(t *testing.T, lang string) map[string]string {
	t.Helper()
	b, err := fs.ReadFile(web.I18n, "static/i18n/admin/draw/"+lang+".json")
	if err != nil {
		t.Fatal(err)
	}
	m := map[string]string{}
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	return m
}

func TestDrawCatalogsHaveSameKeysAndParams(t *testing.T) {
	fr, en := moduleCatalog(t, "fr"), moduleCatalog(t, "en")
	param := regexp.MustCompile(`\{[a-z_]+\}`)
	for k, v := range fr {
		if !strings.HasPrefix(k, "admin.draw.") {
			t.Errorf("clé %q sans préfixe admin.draw.", k)
		}
		w, ok := en[k]
		if !ok {
			t.Errorf("clé %q absente en anglais", k)
			continue
		}
		if strings.Join(param.FindAllString(v, -1), ",") != strings.Join(param.FindAllString(w, -1), ",") {
			t.Errorf("paramètres différents pour %q", k)
		}
		if w == v && len(v) > 12 {
			t.Errorf("%q : texte anglais identique au français", k)
		}
	}
	for k := range en {
		if _, ok := fr[k]; !ok {
			t.Errorf("clé %q absente en français", k)
		}
	}
	for _, k := range []string{keyTitle, keyForbidden, keyInternal, keyConflict, keyInvalid, keyConfirm, keyChoose, keyPending,
		keyInfeasible, keyDateInvalid, keyDateLocked, keyNotDone, keyDone, keyRedone, keyOpened, keyDateSaved} {
		if fr[k] == "" || en[k] == "" {
			t.Errorf("clé de message %q manquante", k)
		}
	}
}

var rawKeyRe = regexp.MustCompile(`\badmin\.draw\.[a-z_]+(\.[a-z_]+)*`)

// F10.AC37 : rendu anglais sans clé brute ni texte français, dans tous les états.
func TestEnglishRenderHasNoRawKeyNorFrench(t *testing.T) {
	e := newEnv(t)
	gid := e.group("anglais")
	e.addN(gid, "Hortensia", 4, participants.RoleNormal)
	e.reg.Pending[gid] = 2
	short := e.group("anglaiscourt")
	e.addN(short, "Seul", 4, participants.RoleUnSeul)
	e.addN(short, "Mamie", 3, participants.RoleReceptionSeule)
	fr, en := moduleCatalog(t, "fr"), moduleCatalog(t, "en")
	ck, ckShort := e.adminCookie(gid), e.adminCookie(short)

	pages := []string{
		"/anglais/admin/tirage", "/anglais/admin/tirage?onglet=ajuster", "/anglais/admin/tirage?onglet=historique",
		"/anglais/admin/tirage?confirmer=lancer", "/anglais/admin/tirage?confirmer=ouvrir",
		"/anglaiscourt/admin/tirage",
	}
	run := func(path string) {
		cookie := ck
		if strings.HasPrefix(path, "/anglaiscourt") {
			cookie = ckShort
		}
		for _, lang := range []string{"en", "fr"} {
			rec := e.get(path, lang, cookie)
			if rec.Code != 200 {
				t.Fatalf("%s : %d", path, rec.Code)
			}
			body := rec.Body.String()
			if m := rawKeyRe.FindString(body); m != "" {
				t.Errorf("%s (%s) : clé brute %q", path, lang, m)
			}
			if lang == "en" {
				unescaped := strings.NewReplacer("&#39;", "'", "&#34;", `"`).Replace(body)
				for k, v := range fr {
					if len(v) >= 8 && en[k] != v && strings.Contains(unescaped, v) {
						t.Errorf("%s : texte français %q (clé %s) dans le rendu anglais", path, v, k)
					}
				}
				if !strings.Contains(body, `lang="en"`) {
					t.Errorf("%s : langue de la page", path)
				}
			}
		}
	}
	for _, p := range pages {
		run(p)
	}
	// Après le tirage : états « fait », ouverture, historique.
	e.reg.Pending[gid] = 0
	e.doDraw(gid)
	e.reveal.targets = 4
	for _, p := range pages[:5] {
		run(p)
	}
	// Les messages d'erreur passent aussi par le catalogue.
	rec := e.post("/anglais/admin/tirage/ouvrir", "/anglais/admin/tirage?confirmer=ouvrir", ck, nil)
	if rec.Code != 422 {
		t.Fatalf("sans confirmation : %d", rec.Code)
	}
	if body := rec.Body.String(); rawKeyRe.MatchString(body) || !strings.Contains(body, `role="alert"`) {
		t.Errorf("erreur mal rendue : %s", body)
	}
}
