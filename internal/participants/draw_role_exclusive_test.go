package participants

import (
	"testing"

	"secretsanta/internal/shared/errs"
)

func TestRoleValuesExclusive(t *testing.T) {
	e := newEnv(t)
	for _, r := range []DrawRole{RoleNormal, RoleReceptionSeule, RoleOffrePlus, RoleUnSeul} {
		if !r.Valid() {
			t.Errorf("%s doit être valide", r)
		}
	}
	for _, r := range []DrawRole{"", "autre", "NORMAL", "normal,un_seul", "reception_seule ", "offre"} {
		if r.Valid() {
			t.Errorf("%q ne doit pas être valide", r)
		}
	}
	// Service : création et réglage refusent toute autre valeur.
	_, err := e.svc.Create(t.Context(), e.admin(), e.g1, Input{FirstName: "A", Name: "B", Role: "autre"})
	wantKind(t, err, errs.KindValidation)
	p := e.add(t, "A", "B")
	wantKind(t, e.svc.SetRole(t.Context(), e.admin(), e.g1, p.ID, "autre"), errs.KindValidation)
	wantKind(t, e.svc.SetRole(t.Context(), e.admin(), e.g1, p.ID, ""), errs.KindValidation)
}

func TestRoleCheckConstraintInSQL(t *testing.T) {
	e := newEnv(t)
	p := e.add(t, "A", "B")
	for _, bad := range []string{"autre", "", "NORMAL", "normal,un_seul"} {
		if _, err := e.db.Exec(`UPDATE participants SET draw_role = ? WHERE id = ?`, bad, p.ID); err == nil {
			t.Errorf("la base doit refuser %q (UPDATE)", bad)
		}
		if _, err := e.db.Exec(`INSERT INTO participants (group_id, name, first_name, name_key, joined_at, draw_role) VALUES (?, 'x', 'y', ?, 'z', ?)`,
			e.g1, "k"+bad, bad); err == nil {
			t.Errorf("la base doit refuser %q (INSERT)", bad)
		}
	}
	if _, err := e.db.Exec(`UPDATE participants SET draw_role = NULL WHERE id = ?`, p.ID); err == nil {
		t.Error("NULL refusé")
	}
	for _, ok := range []string{"normal", "reception_seule", "offre_plus", "un_seul"} {
		if _, err := e.db.Exec(`UPDATE participants SET draw_role = ? WHERE id = ?`, ok, p.ID); err != nil {
			t.Errorf("%s : %v", ok, err)
		}
	}
}
