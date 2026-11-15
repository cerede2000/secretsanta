package groups

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"secretsanta/internal/shared/errs"
)

// F2.AC8 : vérification à temps constant, refus du secret d'un autre
// groupe, même réponse pour un groupe inconnu et un secret faux.

func TestVerifyAdminSecret(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	a := mustCreate(t, e, validInput("famille-a"))
	b := mustCreate(t, e, validInput("famille-b"))

	id, err := e.svc.VerifyAdminSecret(ctx, "famille-a", a.AdminSecret)
	if err != nil || id != a.Group.ID {
		t.Fatalf("bon secret refusé : %d %v", id, err)
	}
	id, err = e.svc.VerifyAdminSecret(ctx, "famille-b", b.AdminSecret)
	if err != nil || id != b.Group.ID {
		t.Fatalf("bon secret refusé : %d %v", id, err)
	}

	var failures []error
	for _, c := range []struct{ slug, secret string }{
		{"famille-a", b.AdminSecret},       // secret d'un autre groupe
		{"famille-b", a.AdminSecret},       // idem
		{"famille-a", "faux"},              // secret faux
		{"famille-a", ""},                  // vide
		{"famille-a", a.AdminSecret[:10]},  // préfixe correct
		{"famille-a", a.AdminSecret + "x"}, // suffixe ajouté
		{"famille-a", strings.ToUpper(a.AdminSecret)},
		{"inconnu", a.AdminSecret}, // groupe inconnu
		{"inconnu", "faux"},
		{"", ""},
		{"Famille-A", a.AdminSecret},
		{"famille-a", strings.Repeat("x", 1<<20)},
	} {
		id, err := e.svc.VerifyAdminSecret(ctx, c.slug, c.secret)
		if err == nil || id != 0 {
			t.Fatalf("%q/%q accepté (%d)", c.slug, c.secret, id)
		}
		wantFrench(t, err, errs.KindUnauthorized)
		failures = append(failures, err)
	}
	for _, f := range failures[1:] {
		if !reflect.DeepEqual(f, failures[0]) {
			t.Fatalf("réponses distinctes : %#v / %#v", f, failures[0])
		}
	}
}

// La primitive à temps constant est appelée une fois, sur deux condensés de
// 32 octets, pour un groupe connu comme pour un groupe inconnu.
func TestVerifyAdminSecretConstantTime(t *testing.T) {
	e := newEnv(t)
	a := mustCreate(t, e, validInput("famille-a"))
	type call struct{ a, b int }
	var calls []call
	orig := constantTimeEq
	constantTimeEq = func(x, y []byte) int {
		calls = append(calls, call{len(x), len(y)})
		return orig(x, y)
	}
	defer func() { constantTimeEq = orig }()

	for _, c := range []struct{ slug, secret string }{
		{"famille-a", a.AdminSecret},
		{"famille-a", ""},
		{"famille-a", a.AdminSecret[:1]},
		{"famille-a", a.AdminSecret[:21]},
		{"inconnu", a.AdminSecret},
		{"inconnu", ""},
		{"x", strings.Repeat("y", 5000)},
	} {
		calls = calls[:0]
		_, _ = e.svc.VerifyAdminSecret(context.Background(), c.slug, c.secret)
		if len(calls) != 1 || calls[0] != (call{32, 32}) {
			t.Fatalf("%q/%d octets : %v", c.slug, len(c.secret), calls)
		}
	}
}
