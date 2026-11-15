package participants

import (
	"testing"

	"secretsanta/internal/shared/errs"
)

func TestNamesUniqueIgnoringCaseAndAccents(t *testing.T) {
	e := newEnv(t)
	e.add(t, "Éloïse", "Dupont")
	for _, v := range [][2]string{{"eloise", "dupont"}, {"ÉLOÏSE", "DUPONT"}, {"  Eloïse ", "Dupont"}, {"Éloïse", "Dupónt"}} {
		_, err := e.svc.Create(t.Context(), e.admin(), e.g1, Input{FirstName: v[0], Name: v[1]})
		wantKind(t, err, errs.KindConflict)
	}
	// Autre groupe : même nom accepté.
	if _, err := e.svc.Create(t.Context(), AdminActor(e.g2), e.g2, Input{FirstName: "Eloise", Name: "Dupont"}); err != nil {
		t.Fatal(err)
	}
	// Prénom différent : accepté.
	e.add(t, "Louis", "Dupont")
}

func TestUniqueConstraintInDatabase(t *testing.T) {
	e := newEnv(t)
	e.add(t, "A", "B")
	_, err := e.db.Exec(`INSERT INTO participants (group_id, name, first_name, name_key, joined_at) VALUES (?, 'x', 'y', ?, 'z')`,
		e.g1, NormalizeName("A", "B"))
	if err == nil {
		t.Fatal("la base doit refuser un doublon de clé de nom")
	}
}
