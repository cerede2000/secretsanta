package invite

import (
	"net/http"
	"testing"

	"secretsanta/internal/participants"
	"secretsanta/internal/shared/errs"
)

// onlyPending renvoie l'unique inscription en attente du groupe.
func onlyPending(t *testing.T, e *env, g int64) Pending {
	t.Helper()
	ps, err := e.store.ListPending(t.Context(), g, e.clock.Now())
	if err != nil {
		t.Fatal(err)
	}
	if len(ps) != 1 {
		t.Fatalf("%d inscriptions en attente, 1 attendue", len(ps))
	}
	return ps[0]
}

func (e *env) setRoleChoice(t *testing.T, g int64, on bool) {
	t.Helper()
	if err := e.svc.SetRoleChoice(t.Context(), admin(g), g, on); err != nil {
		t.Fatal(err)
	}
}

func (e *env) roleChoice(t *testing.T, g int64) bool {
	t.Helper()
	st, err := e.svc.Settings(t.Context(), admin(g), g)
	if err != nil {
		t.Fatal(err)
	}
	return st.RoleChoice
}

// F11d.AC1 : l'option est désactivée par défaut ; désactivée, toute valeur
// de rôle envoyée est ignorée et le rôle vaut normal à l'inscription.
func TestRoleChoiceDisabledByDefaultIgnoresValues(t *testing.T) {
	cases := [][]string{
		{"offre_plus"},
		{"reception_seule"},
		{"un_seul"},
		{"reception_seule", "offre_plus"},
		{"inconnu"},
		nil,
	}
	for _, roles := range cases {
		e := newEnv(t)
		if e.roleChoice(t, e.g1) {
			t.Fatal("option de choix du rôle active par défaut")
		}
		l := e.enable(t, e.g1)
		inv, err := e.svc.Open(t.Context(), "noel", l.Token)
		if err != nil || inv.RoleChoice {
			t.Fatalf("invitation %+v %v : le choix du rôle ne doit pas être proposé", inv, err)
		}
		s := sub("Léa", "Martin", "lea@example.org")
		s.Roles = roles
		r := e.submit(t, "noel", l.Token, s)
		if r.Outcome != OutcomeCodeSent {
			t.Fatalf("rôles %q : issue %+v", roles, r)
		}
		if _, ok := r.Fields[FieldRole]; ok || r.Values.Role != "" {
			t.Fatalf("rôles %q : le rôle a été lu malgré l'option désactivée %+v", roles, r)
		}
		if p := onlyPending(t, e, e.g1); p.Role != participants.RoleNormal {
			t.Fatalf("rôles %q : rôle %q, normal attendu", roles, p.Role)
		}
	}
}

// F11d.AC1 : l'option est modifiable à tout moment (mode désactivé, mode
// actif, après le tirage), seulement par l'administrateur du groupe ; la
// désactiver fait de nouveau ignorer les valeurs.
func TestRoleChoiceToggleAnyTime(t *testing.T) {
	e := newEnv(t)
	ctx := t.Context()

	// Mode désactivé : l'option se règle quand même.
	e.setRoleChoice(t, e.g1, true)
	if !e.roleChoice(t, e.g1) {
		t.Fatal("option non enregistrée avec le mode désactivé")
	}
	// Activer, renouveler, limiter le mode ne la remet pas à zéro.
	l := e.enable(t, e.g1)
	l, err := e.svc.Renew(ctx, admin(e.g1), e.g1)
	if err != nil {
		t.Fatal(err)
	}
	if err := e.svc.SetLimits(ctx, admin(e.g1), e.g1, 10, nil); err != nil {
		t.Fatal(err)
	}
	if !e.roleChoice(t, e.g1) {
		t.Fatal("option perdue par l'activation ou le renouvellement")
	}
	inv, err := e.svc.Open(ctx, "noel", l.Token)
	if err != nil || !inv.RoleChoice {
		t.Fatalf("invitation %+v %v : choix du rôle attendu", inv, err)
	}
	s := sub("Léa", "Martin", "lea@example.org")
	s.Roles = []string{"offre_plus"}
	if r := e.submit(t, "noel", l.Token, s); r.Outcome != OutcomeCodeSent {
		t.Fatalf("issue %+v", r)
	}
	if p := onlyPending(t, e, e.g1); p.Role != participants.RoleOffrePlus {
		t.Fatalf("rôle %q, offre_plus attendu", p.Role)
	}

	// Désactivée de nouveau : la valeur est ignorée, l'inscription déjà en
	// attente garde son rôle.
	e.setRoleChoice(t, e.g1, false)
	s = sub("Noé", "Bernard", "noe@example.org")
	s.Roles = []string{"reception_seule"}
	if r := e.submit(t, "noel", l.Token, s); r.Outcome != OutcomeCodeSent {
		t.Fatalf("issue %+v", r)
	}
	ps, err := e.store.ListPending(ctx, e.g1, e.clock.Now())
	if err != nil || len(ps) != 2 {
		t.Fatalf("en attente %+v %v", ps, err)
	}
	if ps[0].Role != participants.RoleOffrePlus || ps[1].Role != participants.RoleNormal {
		t.Fatalf("rôles %q et %q", ps[0].Role, ps[1].Role)
	}

	// Après le tirage, l'option reste modifiable.
	e.draw.Done[e.g1] = true
	e.setRoleChoice(t, e.g1, true)
	if !e.roleChoice(t, e.g1) {
		t.Fatal("option non modifiable après le tirage")
	}
	// Mode désactivé : l'option est conservée.
	if err := e.svc.Disable(ctx, admin(e.g1), e.g1); err != nil {
		t.Fatal(err)
	}
	if !e.roleChoice(t, e.g1) {
		t.Fatal("option perdue à la désactivation du mode")
	}

	// Seul l'administrateur du groupe la règle.
	if err := e.svc.SetRoleChoice(ctx, participants.Actor{}, e.g1, false); !errs.Is(err, errs.KindUnauthorized) {
		t.Fatalf("anonyme : %v", err)
	}
	for _, a := range []participants.Actor{admin(e.g2), participants.ParticipantActor(e.g1, 1)} {
		if err := e.svc.SetRoleChoice(ctx, a, e.g1, false); !errs.Is(err, errs.KindForbidden) {
			t.Fatalf("%+v : %v", a, err)
		}
	}
	if !e.roleChoice(t, e.g1) {
		t.Fatal("option changée par un tiers")
	}
	if e.roleChoice(t, e.g2) {
		t.Fatal("l'option fuit vers un autre groupe")
	}
}

// F11d.AC1 par le formulaire HTTP : option désactivée, le champ role est
// ignoré.
func TestRoleChoiceDisabledIgnoredOverHTTP(t *testing.T) {
	e := newEnv(t)
	l := e.enable(t, e.g1)
	h := e.server(t)
	form := formOf("Léa", "Martin", "lea@example.org")
	form[FieldRole] = []string{"un_seul", "offre_plus"}
	rec := postForm(t, h, l.Path, form)
	if rec.Code != http.StatusOK {
		t.Fatalf("statut %d : %s", rec.Code, rec.Body.String())
	}
	if p := onlyPending(t, e, e.g1); p.Role != participants.RoleNormal {
		t.Fatalf("rôle %q, normal attendu", p.Role)
	}
}
