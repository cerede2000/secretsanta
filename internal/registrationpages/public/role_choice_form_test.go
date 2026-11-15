package public

import (
	"net/http"
	"strings"
	"testing"
)

// F11e.AC2 : sans l'option, aucun choix de rôle ; avec l'option, la section
// « Un coup de pouce ? (facultatif) » propose deux choix volontaires, aucun
// présélectionné, sans « Je participe normalement » ni « un seul cadeau ».
func TestRoleChoiceHiddenWhenOptionOff(t *testing.T) {
	e := newEnv(t)
	body := e.do(t, http.MethodGet, joinPath, nil, "fr").Body.String()
	for _, w := range []string{"Un coup de pouce", `name="role"`, "roles.js"} {
		if strings.Contains(body, w) {
			t.Errorf("%q ne doit pas apparaître quand l'option est désactivée", w)
		}
	}
}

func TestRoleChoiceShownWithTwoUncheckedChoices(t *testing.T) {
	e := newEnv(t)
	e.invites.roleChoice = true
	body := e.do(t, http.MethodGet, joinPath, nil, "fr").Body.String()
	for _, w := range []string{"Un coup de pouce ?", "(facultatif)",
		"Je ne peux pas offrir de cadeau, je recevrai le mien",
		"J'accepte d'offrir des cadeaux en plus pour ceux qui ne peuvent pas en acheter",
		`value="reception_seule"`, `value="offre_plus"`} {
		if !strings.Contains(body, strings.ReplaceAll(w, "'", "&#39;")) && !strings.Contains(body, w) {
			t.Errorf("%q absent", w)
		}
	}
	if n := strings.Count(body, `name="role"`); n != 2 {
		t.Errorf("%d choix de rôle, attendu 2", n)
	}
	if strings.Contains(body, " checked") {
		t.Error("un choix est présélectionné")
	}
	for _, w := range []string{"Je participe normalement", "un seul cadeau", "un_seul", `value="normal"`} {
		if strings.Contains(strings.ToLower(body), strings.ToLower(w)) {
			t.Errorf("%q ne doit pas être proposé", w)
		}
	}
}

func TestRoleChoiceSubmittedAndKeptOnError(t *testing.T) {
	e := newEnv(t)
	e.invites.roleChoice = true
	form := map[string][]string{"prenom": {"Camille"}, "nom": {"Martin"}, "email": {"c@exemple.fr"}, "role": {"offre_plus"}}
	e.do(t, http.MethodPost, joinPath, form, "fr")
	if e.invites.count() != 1 || len(e.invites.submissions[0].Roles) != 1 || e.invites.submissions[0].Roles[0] != "offre_plus" {
		t.Errorf("rôle non transmis : %+v", e.invites.submissions)
	}
}
