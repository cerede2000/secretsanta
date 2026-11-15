package public

import (
	"net/http"
	"net/url"
	"strings"
	"testing"
)

// F11c.AC14 (part Go) : la langue choisie est transmise au service, qui la
// range comme locale du participant.
func TestSubmissionCarriesTheChosenLanguage(t *testing.T) {
	e := newEnv(t)
	form := url.Values{"prenom": {"Camille"}, "nom": {"Martin"}, "email": {"camille@exemple.fr"}}
	for _, lang := range []string{"en", "fr"} {
		r := e.do(t, http.MethodPost, joinPath, form, lang)
		if r.Code != http.StatusOK {
			t.Fatalf("%s : %d", lang, r.Code)
		}
	}
	if len(e.invites.submissions) != 2 || e.invites.submissions[0].Lang != "en" || e.invites.submissions[1].Lang != "fr" {
		t.Fatalf("langues transmises : %+v", e.invites.submissions)
	}
}

func TestFormShowsTheLanguageSelectorBeforeAnyIdentification(t *testing.T) {
	e := newEnv(t)
	body := e.do(t, http.MethodGet, joinPath, nil, "fr").Body.String()
	if !strings.Contains(body, `action="/langue"`) || !strings.Contains(body, `name="lang"`) {
		t.Fatal("sélecteur de langue absent du formulaire")
	}
}

func TestBackLinkOnlyFollowsTheSameGroupForm(t *testing.T) {
	cases := map[string]string{
		joinPath:                         joinPath,
		"https://evil.example/x":         "",
		"//evil.example/noel/rejoindre/": "",
		"/autre/rejoindre/abc":           "",
		"/noel/rejoindre/abc?x=1":        "",
		"":                               "",
	}
	for in, want := range cases {
		if got := safeBack("noel", in); got != want {
			t.Errorf("safeBack(%q) = %q, attendu %q", in, got, want)
		}
	}
}
