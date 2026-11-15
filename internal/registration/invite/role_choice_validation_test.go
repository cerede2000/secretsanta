package invite

import (
	"html"
	"net/http"
	"strings"
	"testing"

	"secretsanta/internal/participants"
)

// F11d.AC2 : option activée, seuls normal (défaut), reception_seule et
// offre_plus sont acceptés ; un champ absent donne normal.
func TestRoleChoiceAcceptedValues(t *testing.T) {
	cases := []struct {
		roles []string
		want  participants.DrawRole
	}{
		{nil, participants.RoleNormal},
		{[]string{""}, participants.RoleNormal},
		{[]string{"normal"}, participants.RoleNormal},
		{[]string{"reception_seule"}, participants.RoleReceptionSeule},
		{[]string{"offre_plus"}, participants.RoleOffrePlus},
		{[]string{"offre_plus", "offre_plus"}, participants.RoleOffrePlus},
	}
	for _, c := range cases {
		e := newEnv(t)
		e.setRoleChoice(t, e.g1, true)
		l := e.enable(t, e.g1)
		s := sub("Léa", "Martin", "lea@example.org")
		s.Roles = c.roles
		r := e.submit(t, "noel", l.Token, s)
		if r.Outcome != OutcomeCodeSent {
			t.Fatalf("rôles %q : issue %+v", c.roles, r)
		}
		if p := onlyPending(t, e, e.g1); p.Role != c.want {
			t.Fatalf("rôles %q : rôle %q, %q attendu", c.roles, p.Role, c.want)
		}
		if e.codes.count() != 1 || e.codes.issued[0].Role != c.want {
			t.Fatalf("rôles %q : le code émis ne porte pas le rôle", c.roles)
		}
	}
}

// F11d.AC2 : un_seul et toute valeur inconnue sont refusés, sans
// inscription ni code.
func TestRoleChoiceRefusedValues(t *testing.T) {
	cases := [][]string{
		{"un_seul"},
		{"inconnu"},
		{"Offre_plus"},
		{" offre_plus"},
		{"normal", "un_seul"},
		{"offre_plus", "inconnu"},
		{"normal", "offre_plus"},
		{strings.Repeat("x", 5000)},
	}
	for _, roles := range cases {
		e := newEnv(t)
		e.setRoleChoice(t, e.g1, true)
		l := e.enable(t, e.g1)
		s := sub("Léa", "Martin", "lea@example.org")
		s.Roles = roles
		r := e.submit(t, "noel", l.Token, s)
		if r.Outcome != OutcomeInvalid {
			t.Fatalf("rôles %.40q : issue %+v, refus attendu", roles, r)
		}
		p, ok := r.Fields[FieldRole]
		if !ok || p.Code != codeRoleInvalid || p.Message == "" {
			t.Fatalf("rôles %.40q : problème %+v", roles, r.Fields)
		}
		if r.Values.Role != "" {
			t.Fatalf("rôles %.40q : rôle refusé renvoyé %q", roles, r.Values.Role)
		}
		if e.pendingCount(t, e.g1) != 0 || e.codes.count() != 0 {
			t.Fatalf("rôles %.40q : inscription créée malgré le refus", roles)
		}
	}
}

// F11d.AC2 : les autres champs en erreur sont signalés en même temps que le
// rôle.
func TestRoleChoiceRefusedWithOtherFields(t *testing.T) {
	e := newEnv(t)
	e.setRoleChoice(t, e.g1, true)
	l := e.enable(t, e.g1)
	s := sub("", "Martin", "lea@example.org")
	s.Roles = []string{"un_seul"}
	r := e.submit(t, "noel", l.Token, s)
	if r.Outcome != OutcomeInvalid {
		t.Fatalf("issue %+v", r)
	}
	if _, ok := r.Fields[FieldFirstName]; !ok {
		t.Fatal("prénom manquant non signalé")
	}
	if _, ok := r.Fields[FieldRole]; !ok {
		t.Fatal("rôle refusé non signalé")
	}
}

// F11d.AC2 par le formulaire HTTP.
func TestRoleChoiceOverHTTP(t *testing.T) {
	e := newEnv(t)
	e.setRoleChoice(t, e.g1, true)
	l := e.enable(t, e.g1)
	h := e.server(t)

	form := formOf("Léa", "Martin", "lea@example.org")
	form.Set(FieldRole, "un_seul")
	rec := postForm(t, h, l.Path, form)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("un_seul : statut %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), html.EscapeString(msgRoleInvalid)) {
		t.Fatalf("message absent : %s", rec.Body.String())
	}
	if e.pendingCount(t, e.g1) != 0 {
		t.Fatal("inscription créée avec un_seul")
	}

	form.Set(FieldRole, "reception_seule")
	if rec := postForm(t, h, l.Path, form); rec.Code != http.StatusOK {
		t.Fatalf("reception_seule : statut %d %s", rec.Code, rec.Body.String())
	}
	if p := onlyPending(t, e, e.g1); p.Role != participants.RoleReceptionSeule {
		t.Fatalf("rôle %q", p.Role)
	}
}
