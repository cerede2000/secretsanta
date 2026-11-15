package participants

import (
	"strings"
	"testing"

	"secretsanta/internal/shared/errs"
)

func TestInputsAreCleaned(t *testing.T) {
	e := newEnv(t)
	p, err := e.svc.Create(t.Context(), e.admin(), e.g1, Input{
		FirstName: "  Anne \t Marie​ ", Name: "Du\x00pont\n", GiftHint: " un  livre\r\n\r\n\r\n\r\nde cuisine ", Email: " a@b.fr "})
	if err != nil {
		t.Fatal(err)
	}
	if p.FirstName != "Anne Marie" || p.Name != "Dupont" || p.Email != "a@b.fr" {
		t.Fatalf("nettoyage : %+v", p)
	}
	if p.GiftHint != "un livre\n\nde cuisine" {
		t.Fatalf("indice : %q", p.GiftHint)
	}
}

func TestMaxLengths(t *testing.T) {
	e := newEnv(t)
	ok := func(in Input) error { _, err := e.svc.Create(t.Context(), e.admin(), e.g1, in); return err }
	long := func(n int) string { return strings.Repeat("é", n) }
	if err := ok(Input{FirstName: long(FirstNameMaxLen), Name: long(NameMaxLen), GiftHint: long(GiftHintMaxLen)}); err != nil {
		t.Fatalf("limites exactes : %v", err)
	}
	cases := map[string]Input{
		"nom":    {FirstName: "A", Name: long(NameMaxLen + 1)},
		"prénom": {FirstName: long(FirstNameMaxLen + 1), Name: "B"},
		"indice": {FirstName: "A", Name: "C", GiftHint: long(GiftHintMaxLen + 1)},
		"mail":   {FirstName: "A", Name: "D", Email: strings.Repeat("a", EmailMaxLen) + "@b.fr"},
	}
	for k, in := range cases {
		wantKind(t, ok(in), errs.KindValidation)
		_ = k
	}
}
