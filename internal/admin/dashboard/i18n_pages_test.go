package dashboard

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
	b, err := fs.ReadFile(web.I18n, "static/i18n/admin/dashboard/"+lang+".json")
	if err != nil {
		t.Fatal(err)
	}
	m := map[string]string{}
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	return m
}

func TestDashboardCatalogsHaveSameKeysAndParams(t *testing.T) {
	fr, en := moduleCatalog(t, "fr"), moduleCatalog(t, "en")
	param := regexp.MustCompile(`\{[a-z_]+\}`)
	for k, v := range fr {
		if !strings.HasPrefix(k, "admin.dashboard.") {
			t.Errorf("clé %q sans préfixe admin.dashboard.", k)
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
	for _, k := range []string{keyTitleDashboard, keyTitleCheck, keyTitleJournal, keyForbidden, keyNotDone, keyNoPerson, keyHasEmail, keyInternal, keyHandedOK, keyActionUnknown} {
		if fr[k] == "" || en[k] == "" {
			t.Errorf("clé de message %q manquante", k)
		}
	}
}

var rawKeyRe = regexp.MustCompile(`\badmin\.dashboard\.[a-z_]+(\.[a-z_]+)*`)

// F10.AC31 : rendu anglais sans clé brute ni texte français.
func TestEnglishRenderHasNoRawKeyNorFrench(t *testing.T) {
	e := newEnv(t)
	gid := e.group("anglais")
	ids := e.addN(gid, "Hortensia", 3, participants.RoleNormal)
	e.add(gid, "Mamie", "", participants.RoleNormal)
	e.doDraw(gid)
	e.mail(gid, ids[0], "failed")
	e.reg.Pending[gid] = 2
	// Une action d'une autre fonctionnalité, sans texte dans ce module.
	if _, err := e.db.Exec(`INSERT INTO admin_audit (group_id, action, at) VALUES (?, 'action_inconnue', 1)`, gid); err != nil {
		t.Fatal(err)
	}
	if err := e.audit.Record(ctx, admin(gid), gid, "lien_remis", ids[0]); err != nil {
		t.Fatal(err)
	}
	fr, en := moduleCatalog(t, "fr"), moduleCatalog(t, "en")
	ck := e.adminCookie(gid)
	for _, p := range []string{"/anglais/admin", "/anglais/admin/controle", "/anglais/admin/journal"} {
		rec := e.get(p, "en", ck)
		if rec.Code != 200 {
			t.Fatalf("%s : %d", p, rec.Code)
		}
		body := rec.Body.String()
		if m := rawKeyRe.FindString(body); m != "" {
			t.Errorf("%s : clé brute %q", p, m)
		}
		unescaped := strings.NewReplacer("&#39;", "'", "&#34;", `"`).Replace(body)
		for k, v := range fr {
			if len(v) >= 8 && en[k] != v && strings.Contains(unescaped, v) {
				t.Errorf("%s : texte français %q (clé %s) dans le rendu anglais", p, v, k)
			}
		}
		if !strings.Contains(body, `lang="en"`) {
			t.Errorf("%s : langue de la page", p)
		}
		// Les pages françaises, elles, rendent le français.
		frBody := e.get(p, "fr", ck).Body.String()
		if rawKeyRe.MatchString(frBody) {
			t.Errorf("%s : clé brute en français", p)
		}
	}
	// Les messages d'erreur passent aussi par le catalogue.
	rec := e.post("/anglais/admin/personnes/"+"999999"+"/lien-remis", "/anglais/admin", ck)
	if rec.Code != 404 {
		t.Fatalf("personne inconnue : %d", rec.Code)
	}
	if body := rec.Body.String(); rawKeyRe.MatchString(body) || !strings.Contains(body, "role=\"alert\"") {
		t.Errorf("erreur mal rendue : %s", body)
	}
}
