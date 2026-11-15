package roles

import (
	"encoding/json"
	"io/fs"
	"net/url"
	"regexp"
	"strings"
	"testing"

	"secretsanta/internal/participants"
	"secretsanta/internal/shared/ports"
	"secretsanta/web"
)

func moduleCatalog(t *testing.T, lang string) map[string]string {
	t.Helper()
	b, err := fs.ReadFile(web.I18n, "static/i18n/admin/roles/"+lang+".json")
	if err != nil {
		t.Fatal(err)
	}
	m := map[string]string{}
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	return m
}

func TestRolesCatalogsHaveSameKeysAndParams(t *testing.T) {
	fr, en := moduleCatalog(t, "fr"), moduleCatalog(t, "en")
	param := regexp.MustCompile(`\{[a-z_]+\}`)
	for k, v := range fr {
		if !strings.HasPrefix(k, "admin.roles.") {
			t.Errorf("clé %q sans préfixe admin.roles.", k)
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
	for _, c := range []string{"short.trop_de_personnes_a_un_cadeau", "short.plafond_trop_bas", "ok.text", "notice.applied", "notice.future"} {
		for _, suf := range []string{".one", ".other"} {
			if fr["admin.roles."+c+suf] == "" || en["admin.roles."+c+suf] == "" {
				t.Errorf("pluriel %s%s manquant", c, suf)
			}
		}
	}
	for _, r := range allRoles {
		for _, k := range []string{"admin.roles.role.", "admin.roles.help."} {
			if fr[k+string(r)] == "" || en[k+string(r)] == "" {
				t.Errorf("texte %s%s manquant", k, r)
			}
		}
	}
}

var rawKeyRe = regexp.MustCompile(`\badmin\.roles\.[a-z_]+(\.[a-z_]+)+`)

// pagesInEveryState rend le panneau dans ses états : suffisant, insuffisant,
// aperçu après tirage, notices et erreurs.
func pagesInEveryState(t *testing.T, e *env, lang string) map[string]string {
	t.Helper()
	ck := e.adminCookies(t, e.groupA)
	out := map[string]string{}
	a := e.person(t, e.groupA, "Alice", participants.RoleNormal)
	b := e.person(t, e.groupA, "Bruno", participants.RoleNormal)
	e.person(t, e.groupA, "Chloe", participants.RoleNormal)
	e.person(t, e.groupA, "David", participants.RoleReceptionSeule)
	out["suffisant"] = e.get(t, "/noel/admin/roles", lang, ck).Body.String()
	out["notices"] = e.get(t, "/noel/admin/roles?applied=2&future=1", lang, ck).Body.String()
	out["notices-un"] = e.get(t, "/noel/admin/roles?applied=1&future=1", lang, ck).Body.String()
	for name, f := range map[string]url.Values{
		"role-inconnu": {"ids": ids(a), "role": {"roi"}},
		"sans-choix":   {"role": {"normal"}},
		"inconnu":      {"ids": {"999999"}, "role": {"normal"}},
	} {
		out[name] = e.post(t, "noel", f, lang, ck).Body.String()
	}
	e.person(t, e.groupA, "Emma", participants.RoleUnSeul)
	e.person(t, e.groupA, "Farid", participants.RoleReceptionSeule)
	e.person(t, e.groupA, "Gaby", participants.RoleReceptionSeule)
	if _, err := e.db.Exec(`UPDATE participants SET draw_role='un_seul' WHERE group_id=? AND first_name IN ('Alice','Bruno','Chloe')`, e.groupA); err != nil {
		t.Fatal(err)
	}
	out["insuffisant"] = e.get(t, "/noel/admin/roles", lang, ck).Body.String()
	e.state.Done[e.groupA] = true
	e.repair.Previews = map[ports.RuleKind]ports.RepairPreview{ports.RuleRoleChange: {AffectedCount: 1, AlreadyDiscoveredCount: 1, Repairable: true}}
	out["apres-tirage"] = e.get(t, "/noel/admin/roles", lang, ck).Body.String()
	out["apercu"] = e.post(t, "noel", url.Values{"ids": ids(a, b), "role": {"offre_plus"}}, lang, ck).Body.String()
	return out
}

func TestEnglishRenderHasNoRawKeyNorFrench(t *testing.T) {
	fr, en := moduleCatalog(t, "fr"), moduleCatalog(t, "en")
	e := newEnv(t)
	for state, body := range pagesInEveryState(t, e, "en") {
		if !strings.Contains(body, `lang="en"`) {
			t.Errorf("%s : lang=en attendu", state)
		}
		if m := rawKeyRe.FindString(body); m != "" {
			t.Errorf("%s : clé brute %q", state, m)
		}
		for k, v := range fr {
			if len(strings.Fields(v)) >= 3 && en[k] != v && strings.Contains(body, htmlEscape(v)) {
				t.Errorf("%s : texte français de %s dans la page anglaise", state, k)
			}
		}
		for _, w := range []string{"Rôles dans le tirage", "Modifier en lot", "Récapitulatif", "Appliquer", "Changer", "Suffisant", "Insuffisant", "Propositions", "Annuler", "Reçoit seulement", "Un seul cadeau", "Retour"} {
			if strings.Contains(body, w) {
				t.Errorf("%s : %q dans la page anglaise", state, w)
			}
		}
		if state == "suffisant" && !strings.Contains(body, "Roles in the draw") {
			t.Error("titre anglais absent")
		}
	}
}

func TestFrenchRenderHasNoRawKey(t *testing.T) {
	e := newEnv(t)
	pages := pagesInEveryState(t, e, "fr")
	for state, body := range pages {
		if m := rawKeyRe.FindString(body); m != "" {
			t.Errorf("%s : clé brute %q", state, m)
		}
		if strings.Contains(body, "%!") {
			t.Errorf("%s : paramètre mal formé", state)
		}
	}
	checks := map[string]string{
		"insuffisant":  "Insuffisant",
		"apercu":       "Personnes touchées : 2",
		"notices":      "2 rôles modifiés.",
		"notices-un":   "1 rôle modifié.",
		"apres-tirage": "Jamais de nouveau tirage silencieux",
	}
	for state, want := range checks {
		if !strings.Contains(pages[state], want) {
			t.Errorf("%s : %q absent", state, want)
		}
	}
}

func htmlEscape(s string) string {
	return strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&#34;", "'", "&#39;").Replace(s)
}
