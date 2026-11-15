package app

import (
	"strings"
	"testing"
)

// Le bouton d'accueil doit mener à la création de groupe, pas à la page elle-même.
func TestHomeStartButtonLeadsToGroupCreation(t *testing.T) {
	body := get(t, newApp(t), "/").Body.String()
	if !strings.Contains(body, `<a class="button" href="/creer">`) {
		t.Fatalf("le bouton d'accueil ne pointe pas vers /creer : %s", body)
	}
}
