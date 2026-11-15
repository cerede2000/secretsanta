package public

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"secretsanta/internal/mail/queue"
)

// Sans SMTP, la file d'envoi refuse le code de vérification : l'invité doit
// lire la cause et la marche à suivre, jamais « Un problème est survenu ».
func TestMailDisabledGivesClearMessageToGuest(t *testing.T) {
	for _, lang := range []string{"fr", "en"} {
		t.Run(lang, func(t *testing.T) {
			e := newEnv(t)
			// Erreur enrobée, comme le service la renvoie.
			e.invites.submitErr = fmt.Errorf("envoi du code : %w", queue.ErrMailDisabled)
			r := e.do(t, http.MethodPost, joinPath, url.Values{"prenom": {"Camille"}, "nom": {"Durand"}, "courriel": {"c@example.net"}}, lang)
			if r.Code != http.StatusConflict {
				t.Fatalf("statut %d, 409 attendu", r.Code)
			}
			want, other := "contacte l'organisateur", "Un problème est survenu"
			if lang == "en" {
				want, other = "Contact the organiser", "Something went wrong"
			}
			b := strings.ReplaceAll(r.Body.String(), "&#39;", "'")
			if !strings.Contains(strings.ToLower(b), strings.ToLower(want)) {
				t.Errorf("message de cause absent : %s", b)
			}
			if strings.Contains(b, other) {
				t.Errorf("message générique affiché à tort")
			}
		})
	}
}

func TestOtherSubmitErrorsStayGeneric(t *testing.T) {
	e := newEnv(t)
	e.invites.submitErr = fmt.Errorf("base fermée")
	r := e.do(t, http.MethodPost, joinPath, url.Values{"prenom": {"Camille"}}, "fr")
	if r.Code != http.StatusInternalServerError || !strings.Contains(r.Body.String(), "Un problème est survenu") {
		t.Fatalf("%d", r.Code)
	}
}
