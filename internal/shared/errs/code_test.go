package errs

import (
	"errors"
	"testing"
)

func TestCodeOptional(t *testing.T) {
	e := Validation("Nom obligatoire.")
	if e.Code != "" || e.Params != nil {
		t.Fatal("code et paramètres doivent être facultatifs")
	}
	if c, p := CodeOf(e); c != "" || p != nil {
		t.Fatal("CodeOf sans code")
	}
	if c, _ := CodeOf(errors.New("x")); c != "" {
		t.Fatal("erreur non typée")
	}
}

func TestWithCodeKeepsExistingAPI(t *testing.T) {
	cause := errors.New("sql")
	base := Wrap(KindConflict, "Déjà pris.", cause)
	e := base.WithCode("groups.taken", "name", "Noël")
	if e.Error() != "Déjà pris." || UserMessage(e) != "Déjà pris." || e.Kind != KindConflict || !errors.Is(e, cause) {
		t.Fatal("API existante modifiée")
	}
	c, p := CodeOf(e)
	if c != "groups.taken" || p["name"] != "Noël" {
		t.Fatalf("code : %q %v", c, p)
	}
	if base.Code != "" {
		t.Fatal("l'original ne doit pas changer")
	}
	if KindOf(e) != KindConflict || !Is(e, KindConflict) {
		t.Fatal("nature")
	}
}
