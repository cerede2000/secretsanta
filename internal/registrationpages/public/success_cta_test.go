package public

import (
	"net/http"
	"regexp"
	"strings"
	"testing"

	"secretsanta/internal/mail/templates"
)

var ctaRe = regexp.MustCompile(`<a class="([^"]*)" href="([^"]*)">Écrire ma lettre</a>`)

// F11c.AC1 : le bouton principal de l'écran de réussite mène à la page
// d'écriture de la lettre avec bienvenue=1, par code comme par lien.
func TestSuccessPrimaryButtonOpensLetterWithWelcome(t *testing.T) {
	e := newEnv(t)
	for name, rec := range map[string]func() (int, string){
		"code": func() (int, string) {
			r := e.do(t, http.MethodPost, "/noel/inscription/code", codeForm("ref-1", "ABC123"), "fr")
			return r.Code, r.Body.String()
		},
		"lien": func() (int, string) {
			r := e.do(t, http.MethodPost, "/noel/inscription/confirmer/lien", nil, "fr")
			return r.Code, r.Body.String()
		},
	} {
		status, body := rec()
		if status != http.StatusOK {
			t.Fatalf("%s : %d", name, status)
		}
		m := ctaRe.FindStringSubmatch(body)
		if m == nil {
			t.Fatalf("%s : bouton « Écrire ma lettre » introuvable", name)
		}
		if m[2] != "/noel/lettre?bienvenue=1" {
			t.Errorf("%s : href %q", name, m[2])
		}
		if !strings.Contains(m[1], "btn-red") || strings.Contains(m[1], "btn-ghost") {
			t.Errorf("%s : le bouton principal doit être rouge, classes %q", name, m[1])
		}
		// Un seul bouton plein : « Plus tard » est le secondaire.
		if strings.Count(body, "btn-red") != 1 {
			t.Errorf("%s : un seul bouton principal attendu", name)
		}
	}
}

func TestSuccessLetterHrefIsConfigurable(t *testing.T) {
	e := newEnv(t)
	e.pages.o.LetterPath = func(slug string) string { return "/" + slug + "/ma-lettre" }
	r := e.do(t, http.MethodPost, "/noel/inscription/code", codeForm("ref-1", "ABC123"), "fr")
	if !strings.Contains(r.Body.String(), `href="/noel/ma-lettre?bienvenue=1"`) {
		t.Fatal("le chemin de la lettre configuré n'est pas utilisé")
	}
}

// Le courriel de bienvenue porte la même incitation : écrire sa lettre.
func TestWelcomeMailCarriesTheSameLetterPrompt(t *testing.T) {
	cat := realCatalog(t)
	for _, lang := range []string{"fr", "en"} {
		msg, err := templates.Render(cat, templates.Welcome, templates.Data{
			To: "camille@exemple.fr", FirstName: "Camille", GroupName: "Noël en famille",
			ParticipantLang: lang, Link: publicURL + "/noel/acces/abc",
		})
		if err != nil {
			t.Fatal(err)
		}
		want := cat.T(lang, "mail.tpl.welcome.letter", nil)
		if want == "" || want == "mail.tpl.welcome.letter" {
			t.Fatalf("%s : texte d'incitation absent du catalogue", lang)
		}
		if !strings.Contains(msg.Text, want) {
			t.Errorf("%s : le courriel de bienvenue ne contient pas l'incitation à la lettre", lang)
		}
	}
	// L'écran affiche une incitation de même nature (la lettre au Père Noël).
	e := newEnv(t)
	body := e.do(t, http.MethodPost, "/noel/inscription/code", codeForm("ref-1", "ABC123"), "fr").Body.String()
	if !strings.Contains(body, "lettre au Père Noël") {
		t.Error("l'écran de réussite ne parle pas de la lettre")
	}
}
