package templates

import (
	"strings"
	"testing"
)

// F6.AC13 : « code de vérification » et « bienvenue » nomment le destinataire ;
// le premier contient le code, le lien de confirmation en un clic et les 15
// minutes, le second le lien personnel et l'incitation à écrire sa lettre.
func TestVerificationTemplate(t *testing.T) {
	cat := testCatalog(t)
	d := sampleData(Verification)
	m := render(t, cat, Verification, d)
	if len(d.Code) != 6 {
		t.Fatalf("le code d'essai doit faire 6 caractères")
	}
	for _, part := range []string{m.Text, m.HTML} {
		for _, want := range []string{"Bonjour Léa,", "A1B2C3", confirmLink, "15 minutes"} {
			if !strings.Contains(part, want) {
				t.Errorf("%q absent :\n%s", want, part)
			}
		}
	}
	if !m.Essential || m.UnsubscribeURL != "" {
		t.Errorf("la vérification doit être essentielle, sans désinscription")
	}
	buildKind(t, cat, Verification, d)
}

func TestWelcomeTemplate(t *testing.T) {
	cat := testCatalog(t)
	m := render(t, cat, Welcome, sampleData(Welcome))
	for _, part := range []string{m.Text, m.HTML} {
		for _, want := range []string{"Bonjour Léa,", personalLink, "lettre au Père Noël"} {
			if !strings.Contains(part, want) {
				t.Errorf("%q absent :\n%s", want, part)
			}
		}
	}
	if !m.Essential {
		t.Errorf("la bienvenue doit être essentielle")
	}
	buildKind(t, cat, Welcome, sampleData(Welcome))
}
