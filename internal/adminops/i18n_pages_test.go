package adminops_test

import (
	"html"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"testing"
)

var paramRe = regexp.MustCompile(`\{([A-Za-z_]+)\}`)

func paramsOf(s string) string {
	var ps []string
	for _, m := range paramRe.FindAllStringSubmatch(s, -1) {
		ps = append(ps, m[1])
	}
	sort.Strings(ps)
	return strings.Join(ps, ",")
}

// fragments renvoie les morceaux littéraux d'un texte (hors paramètres) assez
// longs pour être significatifs, le plus long d'abord.
func fragments(v string) []string {
	var out []string
	for _, part := range paramRe.Split(v, -1) {
		if part = strings.TrimSpace(part); len([]rune(part)) >= 6 {
			out = append(out, part)
		}
	}
	sort.Slice(out, func(i, j int) bool { return len(out[i]) > len(out[j]) })
	return out
}

var rawKeyRe = regexp.MustCompile(`\b(?:adminops|layout|core)\.[a-z_]+(?:\.[a-z_]+)*`)

// renderAllViews rend chaque vue d'exploitation (données, erreurs, statut,
// mot de passe, duplication, suppression) dans la langue donnée.
func renderAllViews(t *testing.T, lang string) map[string]string {
	t.Helper()
	out := map[string]string{}
	e := newWebEnv(t)
	out["donnees"] = e.get("/famille/admin/donnees", lang).Body.String()
	out["import-erreur"] = e.postFile(t, "/famille/admin/participants/import", lang, []byte(`{"format":"x"}`)).Body.String()
	out["import-role"] = e.postFile(t, "/famille/admin/participants/import", lang,
		[]byte(`{"format":"secretsanta.participants","version":1,"participants":[{"first_name":"A","name":"B","draw_role":"chef"}]}`)).Body.String()
	out["import-ok"] = e.postFile(t, "/famille/admin/participants/import", lang,
		[]byte(`{"format":"secretsanta.participants","version":1,"participants":[{"first_name":"Luc","name":"B"}],"households":[{"name":"F","members":[0]}]}`)).Body.String()
	e.postForm(t, "/famille/admin/sauvegarde", lang, url.Values{})
	out["apres-sauvegarde"] = e.get("/famille/admin/donnees", lang).Body.String()
	out["suppression-erreur"] = e.postForm(t, "/famille/admin/supprimer", lang, url.Values{"confirm_name": {"x"}}).Body.String()
	out["duplication"] = e.postForm(t, "/famille/admin/dupliquer", lang, url.Values{}).Body.String()
	out["mot-de-passe"] = e.postForm(t, "/famille/admin/acces/reinitialiser", lang, url.Values{}).Body.String()

	e2 := newWebEnv(t)
	out["supprime"] = e2.postForm(t, "/famille/admin/supprimer", lang, url.Values{"confirm_name": {"Famille"}}).Body.String()
	for name, body := range out {
		if !strings.Contains(body, "<html") {
			t.Fatalf("%s : pas de page rendue : %.200s", name, body)
		}
	}
	return out
}

func TestCatalogsParity(t *testing.T) {
	fr, en := catalogFile(t, "fr"), catalogFile(t, "en")
	if len(fr) == 0 {
		t.Fatal("catalogue français vide")
	}
	for k, v := range fr {
		if !strings.HasPrefix(k, "adminops.") {
			t.Errorf("clé %q sans préfixe du module", k)
		}
		e, ok := en[k]
		if !ok {
			t.Errorf("clé %q absente en anglais", k)
			continue
		}
		if paramsOf(v) != paramsOf(e) {
			t.Errorf("clé %q : paramètres différents", k)
		}
		if v == e {
			t.Errorf("clé %q identique en français et en anglais", k)
		}
	}
	for k := range en {
		if _, ok := fr[k]; !ok {
			t.Errorf("clé %q absente en français", k)
		}
	}
}

// Chaque clé utilisée par le code ou le gabarit existe dans le catalogue.
func TestEveryUsedKeyExists(t *testing.T) {
	fr := catalogFile(t, "fr")
	for _, k := range []string{
		"adminops.error.import_too_large", "adminops.error.import_format", "adminops.error.import_field",
		"adminops.error.import_email", "adminops.error.import_locale", "adminops.error.import_role",
		"adminops.error.import_name", "adminops.error.import_household", "adminops.error.import_empty",
		"adminops.error.import_missing", "adminops.error.duplicate_slug", "adminops.error.delete_confirm",
		"adminops.error.generic", "adminops.import.done", "adminops.title", "adminops.secret.title",
		"adminops.duplicated.title", "adminops.deleted.title",
	} {
		if _, ok := fr[k]; !ok {
			t.Errorf("clé %q absente", k)
		}
	}
}

// F10b.AC14 : le rendu anglais de chaque page d'exploitation n'a ni clé brute
// ni texte français.
func TestEnglishRenderHasNoRawKeyNorFrench(t *testing.T) {
	fr, en := catalogFile(t, "fr"), catalogFile(t, "en")
	seen := map[string]bool{}
	for name, body := range renderAllViews(t, "en") {
		if !strings.Contains(body, `<html lang="en">`) {
			t.Errorf("%s : lang=en absent", name)
		}
		text := html.UnescapeString(body)
		if m := rawKeyRe.FindString(body); m != "" {
			t.Errorf("%s : clé brute %q", name, m)
		}
		for k, v := range fr {
			if v == en[k] {
				continue
			}
			for _, frag := range fragments(v) {
				if strings.Contains(text, frag) {
					t.Errorf("%s : texte français %q", name, v)
				}
			}
		}
		for k, v := range en {
			if fs := fragments(v); len(fs) > 0 && strings.Contains(text, fs[0]) {
				seen[k] = true
			}
		}
	}
	for _, k := range []string{"adminops.export.button", "adminops.import.button", "adminops.backup.button",
		"adminops.duplicate.label", "adminops.delete.button", "adminops.access.button", "adminops.secret.once",
		"adminops.deleted.home", "adminops.error.import_format", "adminops.error.import_role", "adminops.error.delete_confirm",
		"adminops.import.done", "adminops.backup.last", "adminops.duplicated.open"} {
		if !seen[k] {
			t.Errorf("texte anglais %q absent des rendus", en[k])
		}
	}
}

func TestFrenchRender(t *testing.T) {
	fr := catalogFile(t, "fr")
	body := html.UnescapeString(renderAllViews(t, "fr")["donnees"])
	for _, k := range []string{"adminops.data.title", "adminops.export.button", "adminops.import.button",
		"adminops.backup.button", "adminops.danger.title", "adminops.delete.warning"} {
		if !strings.Contains(body, fr[k]) {
			t.Errorf("texte français %q absent", fr[k])
		}
	}
	if !strings.Contains(body, "Dupliquer pour 2027") || !strings.Contains(body, "Famille 2027") {
		t.Error("plan de duplication absent")
	}
}
