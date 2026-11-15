package people

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
	b, err := fs.ReadFile(web.I18n, "static/i18n/admin/people/"+lang+".json")
	if err != nil {
		t.Fatal(err)
	}
	m := map[string]string{}
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	return m
}

func TestPeopleCatalogsHaveSameKeysAndParams(t *testing.T) {
	fr, en := moduleCatalog(t, "fr"), moduleCatalog(t, "en")
	param := regexp.MustCompile(`\{[a-z_]+\}`)
	for k, v := range fr {
		if !strings.HasPrefix(k, "admin.people.") {
			t.Errorf("clé %q sans préfixe admin.people.", k)
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

// Toute clé citée par un gabarit ou par le code existe dans les deux langues.
func TestEveryKeyUsedExistsInBothLanguages(t *testing.T) {
	fr, en := moduleCatalog(t, "fr"), moduleCatalog(t, "en")
	has := func(m map[string]string, k string) bool {
		if _, ok := m[k]; ok {
			return true
		}
		_, a := m[k+".one"]
		_, b := m[k+".other"]
		return a && b
	}
	re := regexp.MustCompile(`"(admin\.people\.[a-z_.0-9]+)"`)
	var sources []string
	for _, d := range []string{"templates/admin/people", "templates/admin/roles"} {
		entries, err := fs.ReadDir(web.Templates, d)
		if err != nil {
			t.Fatal(err)
		}
		for _, en := range entries {
			b, _ := fs.ReadFile(web.Templates, d+"/"+en.Name())
			sources = append(sources, string(b))
		}
	}
	for _, f := range []string{"people.go", "list.go", "sheet.go", "rules.go"} {
		b, err := fs.ReadFile(goSources, f)
		if err != nil {
			t.Fatal(err)
		}
		sources = append(sources, string(b))
	}
	n := 0
	for _, src := range sources {
		for _, m := range re.FindAllStringSubmatch(src, -1) {
			k := m[1]
			if strings.HasSuffix(k, ".") { // préfixe de clé construite
				continue
			}
			n++
			if !has(fr, k) || !has(en, k) {
				t.Errorf("clé %q absente d'une langue", k)
			}
		}
	}
	if n < 50 {
		t.Fatalf("clés vérifiées : %d", n)
	}
	// Clés construites : états d'accès, boîtes de notice, types de désinscription.
	for _, k := range []string{
		"filter.access.envoye", "filter.access.echec", "filter.access.en_attente", "filter.access.aucun", "filter.access.ouvert", "filter.access.jamais",
		"notice.added", "notice.edited", "notice.deleted", "notice.rule", "notice.removed", "notice.resent", "notice.corrected",
		"sheet.unsub.relances", "sheet.unsub.nouvelle_idee",
	} {
		if fr["admin.people."+k] == "" || en["admin.people."+k] == "" {
			t.Errorf("texte %s manquant", k)
		}
	}
}

var rawKeyRe = regexp.MustCompile(`\badmin\.people\.[a-z_]+(\.[a-z_]+)+`)

// pagesInEveryState rend les pages du module dans leurs états.
func pagesInEveryState(t *testing.T, e *env, lang string) map[string]string {
	t.Helper()
	ck := e.adminCookies(t, e.groupA)
	out := map[string]string{}
	a := e.person(t, e.groupA, "Alice", "alice@exemple.fr", participants.RoleNormal)
	b := e.person(t, e.groupA, "Bruno", "bruno@exemple.fr", participants.RoleUnSeul)
	c := e.person(t, e.groupA, "Chloe", "", participants.RoleReceptionSeule)
	e.queueRow(t, e.groupA, a, "sent", t0)
	e.queueRow(t, e.groupA, b, "failed", t0)
	e.openSession(t, e.groupA, a, t0)
	if err := e.unsub.Unsubscribe(t.Context(), e.groupA, a, "relances"); err != nil {
		t.Fatal(err)
	}
	p := "/noel/admin/participants"
	out["liste"] = e.get(t, p, lang, ck).Body.String()
	out["liste-filtre"] = e.get(t, p+"?q=bru&acces=echec&role=un_seul&sans_courriel=1", lang, ck).Body.String()
	out["liste-vide"] = e.get(t, p+"?q=personne-inconnue", lang, ck).Body.String()
	out["suppression"] = e.get(t, p+"?supprimer="+sid(a), lang, ck).Body.String()
	out["fiche"] = e.get(t, p+"/"+sid(a), lang, ck).Body.String()
	out["fiche-echec"] = e.get(t, p+"/"+sid(b), lang, ck).Body.String()
	out["fiche-sans-courriel"] = e.get(t, p+"/"+sid(c), lang, ck).Body.String()
	out["fiche-regeneration"] = e.get(t, p+"/"+sid(a)+"?regenerer=1", lang, ck).Body.String()
	out["modification"] = e.get(t, p+"/"+sid(a)+"/modifier", lang, ck).Body.String()
	out["lien-regenere"] = e.post(t, p+"/"+sid(c)+"/regenerer", url.Values{}, lang, ck).Body.String()
	out["erreur-doublon"] = e.post(t, p, url.Values{"first_name": {"Alice"}, "name": {"ZxAliceovitch"}}, lang, ck).Body.String()
	out["erreur-adresse"] = e.post(t, p, url.Values{"first_name": {"Z"}, "name": {"Y"}, "email": {"x"}}, lang, ck).Body.String()
	out["erreur-sans-courriel"] = e.post(t, p+"/"+sid(c)+"/renvoyer", url.Values{}, lang, ck).Body.String()
	out["erreur-adresse-fiche"] = e.post(t, p+"/"+sid(a)+"/adresse", url.Values{"email": {"x"}}, lang, ck).Body.String()
	out["erreur-regle"] = e.post(t, p+"/regles", url.Values{"kind": {"exclusion"}, "a": {sid(a)}, "b": {sid(a)}}, lang, ck).Body.String()
	e.seedDraw(t, e.groupA, []int64{a, b, c})
	e.repair.Previews = map[ports.RuleKind]ports.RepairPreview{
		ports.RuleExclusion: {AffectedCount: 1, AlreadyDiscoveredCount: 1, Repairable: true},
		ports.RuleForcing:   {AffectedCount: 1, AlreadyDiscoveredCount: 1, Repairable: true},
	}
	out["apres-tirage"] = e.get(t, p, lang, ck).Body.String()
	out["suppression-apres-tirage"] = e.post(t, p+"/"+sid(a)+"/supprimer", url.Values{"confirm": {"1"}}, lang, ck).Body.String()
	out["apercu-regle"] = e.post(t, p+"/regles", url.Values{"kind": {"forced"}, "a": {sid(a)}, "b": {sid(c)}}, lang, ck).Body.String()
	return out
}

func TestEnglishRenderHasNoRawKeyNorFrench(t *testing.T) {
	fr, en := moduleCatalog(t, "fr"), moduleCatalog(t, "en")
	e := newEnv(t)
	pages := pagesInEveryState(t, e, "en")
	for state, body := range pages {
		if !strings.Contains(body, `lang="en"`) {
			t.Errorf("%s : lang=en attendu (%.80s)", state, body)
			continue
		}
		if m := rawKeyRe.FindString(body); m != "" {
			t.Errorf("%s : clé brute %q", state, m)
		}
		for k, v := range fr {
			if len(strings.Fields(v)) >= 3 && en[k] != v && strings.Contains(body, htmlEscape(v)) {
				t.Errorf("%s : texte français de %s dans la page anglaise", state, k)
			}
		}
		for _, w := range []string{"Rechercher", "Ajouter", "Supprimer", "Annuler", "Renvoyer", "Régénérer", "Désinscrit", "Foyer", "Statut d", "Courriel envoyé", "Échec", "Retour"} {
			if strings.Contains(body, w) {
				t.Errorf("%s : %q dans la page anglaise", state, w)
			}
		}
	}
	if !strings.Contains(pages["liste"], "Add someone") || !strings.Contains(pages["fiche"], "Access status") {
		t.Error("textes anglais attendus absents")
	}
	if !strings.Contains(pages["apercu-regle"], "People affected: 1") {
		t.Error("aperçu en comptes attendu après tirage")
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
		"liste":                    "Ajouter quelqu&#39;un",
		"suppression":              "Retirer Alice ZxAliceovitch ?",
		"suppression-apres-tirage": "Ajuster le tirage",
		"fiche-regeneration":       "Régénérer le lien ?",
		"erreur-doublon":           "porte déjà ce prénom",
		"erreur-sans-courriel":     "n&#39;a pas d&#39;adresse mail",
		"apercu-regle":             "Personnes touchées : 1",
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
