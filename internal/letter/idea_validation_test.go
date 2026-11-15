package letter

import (
	"strings"
	"testing"

	"secretsanta/internal/shared/errs"
)

func TestIdeaInputsCleanedAndBounded(t *testing.T) {
	e := newEnv(t)
	p := e.person(t, "Alice", "normal")
	ctx := t.Context()
	id, err := e.svc.AddIdea(ctx, e.group, p, IdeaInput{
		Title: "  Un​   beau\x00  livre \n", URL: " https://exemple.fr/a?b=1 ", Text: "ligne 1\r\n\r\n\r\n\r\nligne\t2",
	})
	if err != nil {
		t.Fatal(err)
	}
	l, _ := e.svc.Get(ctx, e.group, p)
	if l.Ideas[0].ID != id || l.Ideas[0].Title != "Un beau livre" || l.Ideas[0].Text != "ligne 1\n\nligne 2" || l.Ideas[0].URL != "https://exemple.fr/a?b=1" {
		t.Fatalf("saisie mal nettoyée : %+v", l.Ideas[0])
	}
	bad := []IdeaInput{
		{Title: ""}, {Title: "   "},
		{Title: strings.Repeat("é", TitleMax+1)},
		{Title: "t", Text: strings.Repeat("a", TextMax+1)},
		{Title: "t", URL: "https://exemple.fr/" + strings.Repeat("a", URLMax)},
		{Title: "t", URL: "javascript:alert(1)"},
		{Title: "t", URL: "data:text/html,x"},
		{Title: "t", URL: "ftp://exemple.fr/x"},
		{Title: "t", URL: "file:///etc/passwd"},
		{Title: "t", URL: "//exemple.fr"},
		{Title: "t", URL: "exemple.fr"},
		{Title: "t", URL: "http://"},
		{Title: "t", URL: "https://user:pw@exemple.fr"},
		{Title: "t", URL: "JaVaScRiPt:alert(1)"},
	}
	for _, in := range bad {
		if _, err := e.svc.AddIdea(ctx, e.group, p, in); !errs.Is(err, errs.KindValidation) {
			t.Errorf("%+v devrait être refusée : %v", in, err)
		}
	}
	for _, ok := range []string{"http://exemple.fr", "HTTPS://Exemple.fr/x"} {
		if _, err := e.svc.AddIdea(ctx, e.group, p, IdeaInput{Title: "t", URL: ok}); err != nil {
			t.Errorf("%q devrait passer : %v", ok, err)
		}
	}
	if _, err := e.svc.SetIntro(ctx, e.group, p, strings.Repeat("a", IntroMax+1)); !errs.Is(err, errs.KindValidation) {
		t.Fatalf("introduction trop longue : %v", err)
	}
	l, err = e.svc.SetIntro(ctx, e.group, p, "  Cher​ Père   Noël ")
	if err != nil || l.Intro != "Cher Père Noël" {
		t.Fatalf("%v %q", err, l.Intro)
	}
}
