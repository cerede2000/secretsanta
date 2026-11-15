package public

import (
	"encoding/json"
	"io/fs"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"testing"
	"time"

	"secretsanta/internal/registration/invite"
	"secretsanta/web"
)

func moduleCatalog(t *testing.T, lang string) map[string]string {
	t.Helper()
	b, err := fs.ReadFile(web.I18n, "static/i18n/registration/public/"+lang+".json")
	if err != nil {
		t.Fatal(err)
	}
	m := map[string]string{}
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	return m
}

// F11c.AC10 : mêmes clés, mêmes paramètres, préfixe du module.
func TestPublicCatalogsHaveSameKeysAndParams(t *testing.T) {
	fr, en := moduleCatalog(t, "fr"), moduleCatalog(t, "en")
	param := regexp.MustCompile(`\{[a-z_]+\}`)
	for k, v := range fr {
		if !strings.HasPrefix(k, "registration.public.") {
			t.Errorf("clé %q sans préfixe registration.public.", k)
		}
		w, ok := en[k]
		if !ok {
			t.Errorf("clé %q absente en anglais", k)
			continue
		}
		if params(param, v) != params(param, w) {
			t.Errorf("paramètres différents pour %q", k)
		}
	}
	for k := range en {
		if _, ok := fr[k]; !ok {
			t.Errorf("clé %q absente en français", k)
		}
	}
}

// params liste les paramètres d'un texte, sans tenir compte de leur ordre.
func params(re *regexp.Regexp, s string) string {
	p := re.FindAllString(s, -1)
	sort.Strings(p)
	return strings.Join(p, ",")
}

var rawKeyRe = regexp.MustCompile(`\bregistration\.[a-z_]+(\.[a-z_0-9]+)*`)

func htmlEscape(s string) string {
	return strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&#34;", "'", "&#39;").Replace(s)
}

// Toutes les clés employées par les gabarits existent dans les catalogues et
// la page anglaise ne contient ni clé brute ni texte français.
func TestEnglishRenderHasNoRawKeyNorFrench(t *testing.T) {
	e := newEnv(t)
	e.group.BudgetCents = cents(2500)
	e.group.RevealAt = at(2026, time.December, 1, 18)
	fr := map[string]string{}
	en := map[string]string{}
	for _, l := range []struct {
		lang string
		dst  map[string]string
	}{{"fr", fr}, {"en", en}} {
		for _, f := range []string{"registration/public", "registration"} {
			b, err := fs.ReadFile(web.I18n, "static/i18n/"+f+"/"+l.lang+".json")
			if err != nil {
				t.Fatal(err)
			}
			m := map[string]string{}
			if err := json.Unmarshal(b, &m); err != nil {
				t.Fatal(err)
			}
			for k, v := range m {
				l.dst[k] = v
			}
		}
	}
	type step struct {
		name   string
		method string
		path   string
		form   url.Values
		prep   func()
	}
	person := url.Values{"prenom": {"Camille"}, "nom": {"Martin"}, "email": {"c@exemple.fr"}}
	steps := []step{
		{"formulaire", http.MethodGet, joinPath, nil, nil},
		{"champs invalides", http.MethodPost, joinPath, person, func() {
			e.invites.result = &invite.Result{Outcome: invite.OutcomeInvalid, Fields: map[string]invite.Problem{
				invite.FieldFirstName: {Code: "registration.first_name_required"},
				invite.FieldName:      {Code: "registration.name_too_long"},
				invite.FieldEmail:     {Code: "registration.email_invalid"},
			}}
		}},
		{"nom déjà inscrit", http.MethodPost, joinPath, person, func() {
			e.invites.result = &invite.Result{Outcome: invite.OutcomeNameTaken,
				Fields: map[string]invite.Problem{invite.FieldName: {Code: "registration.name_taken"}}}
		}},
		{"adresse partagée", http.MethodPost, joinPath, person, func() {
			e.invites.result = &invite.Result{Outcome: invite.OutcomeConfirmShared}
		}},
		{"code envoyé", http.MethodPost, joinPath, person, func() { e.invites.result = nil }},
		{"code refusé", http.MethodPost, "/noel/inscription/code", codeForm("ref-1", "ZZZZZZ"), nil},
		{"réussite", http.MethodPost, "/noel/inscription/code", codeForm("ref-1", "ABC123"), nil},
		{"confirmation (GET)", http.MethodGet, "/noel/inscription/confirmer/lien", nil, nil},
		{"confirmation (POST)", http.MethodPost, "/noel/inscription/confirmer/lien", nil, nil},
		{"lien refusé", http.MethodGet, "/noel/inscription/confirmer/faux", nil, nil},
		{"clos : tirage", http.MethodGet, joinPath, nil, func() { e.invites.closed = invite.ClosedDraw }},
		{"clos : complet", http.MethodGet, joinPath, nil, func() { e.invites.closed = invite.ClosedFull }},
		{"clos : date", http.MethodGet, joinPath, nil, func() { e.invites.closed = invite.ClosedDate }},
	}
	for _, s := range steps {
		if s.prep != nil {
			s.prep()
		}
		for _, lang := range []string{"en", "fr"} {
			rec := e.do(t, s.method, s.path, s.form, lang)
			body := rec.Body.String()
			if rec.Code >= 500 {
				t.Fatalf("%s : %d", s.name, rec.Code)
			}
			if m := rawKeyRe.FindString(body); m != "" {
				t.Errorf("%s (%s) : clé brute %q", s.name, lang, m)
			}
			if !strings.Contains(body, `lang="`+lang+`"`) {
				t.Errorf("%s : lang=%s attendu", s.name, lang)
			}
			if lang != "en" {
				continue
			}
			for k, v := range fr {
				if len(strings.Fields(v)) >= 3 && en[k] != v && strings.Contains(body, htmlEscape(v)) {
					t.Errorf("%s : texte français de %s dans la page anglaise", s.name, k)
				}
			}
			for _, w := range []string{"Écrire ma lettre", "Tu es inscrit", "Prénom", "Recevoir mon code", "Rejoins", "Vérifie ton mail", "décembre", "Plus tard"} {
				if strings.Contains(body, w) {
					t.Errorf("%s : %q dans la page anglaise", s.name, w)
				}
			}
		}
	}
}

// Les clés citées par les gabarits et par le code existent en français et en
// anglais.
func TestKeysUsedByTemplatesExist(t *testing.T) {
	keyRe := regexp.MustCompile(`"(registration\.[a-z_]+(?:\.[a-z_0-9]+)*)"`)
	all := map[string]bool{}
	for _, f := range []string{"registration/public/fr", "registration/public/en", "registration/fr", "registration/en"} {
		b, err := fs.ReadFile(web.I18n, "static/i18n/"+f+".json")
		if err != nil {
			t.Fatal(err)
		}
		m := map[string]string{}
		if err := json.Unmarshal(b, &m); err != nil {
			t.Fatal(err)
		}
		for k := range m {
			all[f[len(f)-2:]+":"+k] = true
		}
	}
	entries, err := fs.ReadDir(web.Templates, "templates/registration/public")
	if err != nil || len(entries) < 5 {
		t.Fatalf("gabarits : %v (%d)", err, len(entries))
	}
	for _, en := range entries {
		b, _ := fs.ReadFile(web.Templates, "templates/registration/public/"+en.Name())
		for _, m := range keyRe.FindAllStringSubmatch(string(b), -1) {
			for _, lang := range []string{"fr", "en"} {
				if !all[lang+":"+m[1]] {
					t.Errorf("%s : clé %q absente (%s)", en.Name(), m[1], lang)
				}
			}
		}
	}
}
