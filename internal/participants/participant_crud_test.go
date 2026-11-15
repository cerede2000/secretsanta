package participants

import (
	"testing"

	"secretsanta/internal/shared/errs"
)

func TestCreateWithoutEmail(t *testing.T) {
	e := newEnv(t)
	p, err := e.svc.Create(t.Context(), e.admin(), e.g1, Input{FirstName: "Léa", Name: "Martin"})
	if err != nil {
		t.Fatal(err)
	}
	if p.Email != "" || p.Name != "Martin" || p.FirstName != "Léa" {
		t.Fatalf("participant inattendu : %+v", p)
	}
	got, err := e.svc.Get(t.Context(), e.admin(), e.g1, p.ID)
	if err != nil || got.Name != "Martin" || got.Email != "" {
		t.Fatalf("relecture : %+v %v", got, err)
	}
}

func TestInvalidEmailRefused(t *testing.T) {
	e := newEnv(t)
	for _, bad := range []string{"pas-un-mail", "a@b", "a b@c.fr", "@c.fr", "Léa <lea@c.fr>", "a@.fr"} {
		_, err := e.svc.Create(t.Context(), e.admin(), e.g1, Input{FirstName: "A", Name: "B" + bad, Email: bad})
		if !errs.Is(err, errs.KindValidation) {
			t.Errorf("%q devrait être refusé, obtenu %v", bad, err)
		}
	}
	if _, err := e.svc.Create(t.Context(), e.admin(), e.g1, Input{FirstName: "A", Name: "B", Email: "a@b.fr"}); err != nil {
		t.Fatal(err)
	}
}

func TestRequiredNames(t *testing.T) {
	e := newEnv(t)
	_, err := e.svc.Create(t.Context(), e.admin(), e.g1, Input{FirstName: "", Name: "X"})
	wantKind(t, err, errs.KindValidation)
	_, err = e.svc.Create(t.Context(), e.admin(), e.g1, Input{FirstName: "X", Name: "  "})
	wantKind(t, err, errs.KindValidation)
}
