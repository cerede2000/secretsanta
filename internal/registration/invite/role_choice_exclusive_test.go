package invite

import (
	"html"
	"net/http"
	"strings"
	"testing"

	"secretsanta/internal/participants"
)

// F11d.AC4 : reception_seule et offre_plus envoyés ensemble sont refusés
// avec un message intégré au champ ; aucune inscription n'est créée.
func TestRoleChoiceExclusivePairRefused(t *testing.T) {
	for _, roles := range [][]string{
		{"reception_seule", "offre_plus"},
		{"offre_plus", "reception_seule"},
		{"normal", "reception_seule", "offre_plus"},
	} {
		e := newEnv(t)
		e.setRoleChoice(t, e.g1, true)
		l := e.enable(t, e.g1)
		s := sub("Léa", "Martin", "lea@example.org")
		s.Roles = roles
		r := e.submit(t, "noel", l.Token, s)
		if r.Outcome != OutcomeInvalid || r.Problem.Code != codeInvalid {
			t.Fatalf("rôles %q : issue %+v", roles, r)
		}
		p, ok := r.Fields[FieldRole]
		if !ok || p.Code != codeRoleExclusive || p.Message != msgRoleExclusive {
			t.Fatalf("rôles %q : problème du champ %+v", roles, r.Fields)
		}
		if r.Values.FirstName != "Léa" || r.Values.Email != "lea@example.org" {
			t.Fatalf("saisies non renvoyées %+v", r.Values)
		}
		if e.pendingCount(t, e.g1) != 0 || e.codes.count() != 0 {
			t.Fatalf("rôles %q : inscription créée malgré le refus", roles)
		}
	}
}

// F11d.AC4 : l'absence de choix donne normal.
func TestRoleChoiceAbsentGivesNormal(t *testing.T) {
	e := newEnv(t)
	e.setRoleChoice(t, e.g1, true)
	l := e.enable(t, e.g1)
	r := e.submit(t, "noel", l.Token, sub("Léa", "Martin", "lea@example.org"))
	if r.Outcome != OutcomeCodeSent {
		t.Fatalf("issue %+v", r)
	}
	if p := onlyPending(t, e, e.g1); p.Role != participants.RoleNormal {
		t.Fatalf("rôle %q, normal attendu", p.Role)
	}
}

// F11d.AC4 par le formulaire HTTP : deux valeurs du champ role, réponse 422
// avec le message, aucune inscription.
func TestRoleChoiceExclusiveOverHTTP(t *testing.T) {
	e := newEnv(t)
	e.setRoleChoice(t, e.g1, true)
	l := e.enable(t, e.g1)
	h := e.server(t)
	form := formOf("Léa", "Martin", "lea@example.org")
	form[FieldRole] = []string{"reception_seule", "offre_plus"}
	rec := postForm(t, h, l.Path, form)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("statut %d", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, html.EscapeString(msgRoleExclusive)) || !strings.Contains(body, `data-field="role"`) {
		t.Fatalf("message intégré absent : %s", body)
	}
	if e.pendingCount(t, e.g1) != 0 || e.codes.count() != 0 {
		t.Fatal("inscription créée malgré le refus")
	}
}
