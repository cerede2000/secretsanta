package adminconsult

import (
	"encoding/json"
	"io/fs"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"testing"

	"secretsanta/internal/participants"
	"secretsanta/web"
)

func moduleCatalog(t *testing.T, lang string) map[string]string {
	t.Helper()
	b, err := fs.ReadFile(web.I18n, "static/i18n/adminconsult/"+lang+".json")
	if err != nil {
		t.Fatal(err)
	}
	m := map[string]string{}
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	return m
}

// F10b.AC12 : catalogues français et anglais, mêmes clés préfixées par le
// module, mêmes paramètres, et chaque clé utilisée par le code existe.
func TestConsultCatalogsHaveSameKeysAndParams(t *testing.T) {
	fr, en := moduleCatalog(t, "fr"), moduleCatalog(t, "en")
	param := regexp.MustCompile(`\{[a-z_]+\}`)
	for k, v := range fr {
		if !strings.HasPrefix(k, "adminconsult.") {
			t.Errorf("clé %q sans préfixe adminconsult.", k)
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
	for _, k := range []string{keyTitle, keyJournalTitle, keyBadPassword, keyNoPassword, keyChoose, keyNoDraw, keyLimited,
		keyInternal, keyForbidden, keyInvalid} {
		if fr[k] == "" || en[k] == "" {
			t.Errorf("clé de message %q manquante", k)
		}
	}
	// Toute clé citée par le gabarit existe dans les deux langues.
	tpl, err := fs.ReadFile(web.Templates, "templates/adminconsult/consult.html")
	if err != nil {
		t.Fatal(err)
	}
	used := regexp.MustCompile(`\{\{tn? "(adminconsult\.[a-z_.]+)"`).FindAllStringSubmatch(string(tpl), -1)
	if len(used) == 0 {
		t.Fatal("le gabarit ne cite aucune clé")
	}
	for _, m := range used {
		k := m[1]
		if fr[k] == "" && fr[k+".one"] == "" {
			t.Errorf("clé %q du gabarit absente du catalogue", k)
		}
		if strings.Contains(string(tpl), `{{tn "`+k+`"`) && (fr[k+".one"] == "" || fr[k+".other"] == "" || en[k+".one"] == "" || en[k+".other"] == "") {
			t.Errorf("clé plurielle %q incomplète", k)
		}
	}
}

var rawKeyRe = regexp.MustCompile(`\badminconsult\.[a-z_]+(\.[a-z_]+)*`)

// F10b.AC12 : rendu anglais sans clé brute ni texte français, dans tous les
// états de la page de consultation et du journal.
func TestEnglishRenderHasNoRawKeyNorFrench(t *testing.T) {
	e := newEnv(t)
	gid := e.group("anglais", true)
	ids := e.addN(gid, "Hortense", 4)
	reception := e.add(gid, "Mamie", participants.RoleReceptionSeule)
	early := e.group("anglais-avant", true)
	earlyIDs := e.addN(early, "Ines", 3)
	fr, en := moduleCatalog(t, "fr"), moduleCatalog(t, "en")
	ck, ckEarly := e.adminCookie(gid), e.adminCookie(early)

	check := func(what, body, lang string) {
		t.Helper()
		if m := rawKeyRe.FindString(body); m != "" {
			t.Errorf("%s (%s) : clé brute %q", what, lang, m)
		}
		if lang != "en" {
			return
		}
		unescaped := strings.NewReplacer("&#39;", "'", "&#34;", `"`).Replace(body)
		for k, v := range fr {
			if len(v) >= 8 && en[k] != v && strings.Contains(unescaped, v) {
				t.Errorf("%s : texte français %q (clé %s) dans le rendu anglais", what, v, k)
			}
		}
		if !strings.Contains(body, `lang="en"`) {
			t.Errorf("%s : langue de la page", what)
		}
	}
	for _, lang := range []string{"en", "fr"} {
		// Formulaire vierge.
		rec := e.get("/anglais/admin/consultation", lang, ck)
		if rec.Code != http.StatusOK {
			t.Fatalf("formulaire : %d", rec.Code)
		}
		check("formulaire", rec.Body.String(), lang)
		// Avant le tirage : erreur générale.
		rec = e.consult("anglais-avant", lang, ckEarly, url.Values{"person": {idStr(earlyIDs[0])}, "password": {e.secrets[early]}})
		if rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), `role="alert"`) {
			t.Fatalf("avant le tirage : %d", rec.Code)
		}
		check("avant le tirage", rec.Body.String(), lang)
		// Erreurs sous les champs.
		rec = e.consult("anglais", lang, ck, url.Values{})
		if rec.Code != http.StatusUnprocessableEntity {
			t.Fatalf("champs vides : %d", rec.Code)
		}
		check("champs vides", rec.Body.String(), lang)
		// Journal vide (participant, puis administrateur).
		rec = e.get("/anglais/consultations", lang, e.participantCookie(gid, ids[0]))
		check("journal vide", rec.Body.String(), lang)
	}
	e.doDraw(gid)
	for _, lang := range []string{"en", "fr"} {
		rec := e.consult("anglais", lang, ck, url.Values{"person": {idStr(ids[0])}, "password": {"faux"}})
		if rec.Code != http.StatusForbidden {
			t.Fatalf("mot de passe faux : %d", rec.Code)
		}
		check("mot de passe faux", rec.Body.String(), lang)
		for _, who := range []int64{ids[0], reception} {
			rec = e.consult("anglais", lang, ck, url.Values{"person": {idStr(who)}, "password": {e.secrets[gid]}})
			if rec.Code != http.StatusOK {
				t.Fatalf("résultat : %d", rec.Code)
			}
			check("résultat", rec.Body.String(), lang)
		}
		rec = e.get("/anglais/consultations", lang, e.participantCookie(gid, ids[1]))
		if rec.Code != http.StatusOK {
			t.Fatalf("journal : %d", rec.Code)
		}
		check("journal", rec.Body.String(), lang)
		rec = e.get("/anglais/consultations", lang, ck)
		check("journal administrateur", rec.Body.String(), lang)
	}
	// Le texte attendu est bien rendu en anglais.
	rec := e.get("/anglais/consultations", "en", e.participantCookie(gid, ids[2]))
	if !strings.Contains(rec.Body.String(), "The organiser viewed a pair.") {
		t.Fatal("journal anglais : ligne absente")
	}
}
