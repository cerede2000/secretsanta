package groupsettings

import (
	"context"
	"testing"

	"secretsanta/internal/shared/errs"
)

func TestDefaultLanguage(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	if _, ok := e.svc.GroupLanguage(ctx, e.slug); ok {
		t.Fatal("valeur absente attendue")
	}
	if _, ok, err := e.svc.DefaultLanguage(ctx, e.gid); err != nil || ok {
		t.Fatalf("absente attendue : %v %v", ok, err)
	}
	if err := e.svc.SetDefaultLanguage(ctx, e.gid, "xx"); !errs.Is(err, errs.KindValidation) {
		t.Fatalf("langue non chargée refusée : %v", err)
	}
	if err := e.svc.SetDefaultLanguage(ctx, e.gid, "en"); err != nil {
		t.Fatal(err)
	}
	if l, ok, err := e.svc.DefaultLanguage(ctx, e.gid); err != nil || !ok || l != "en" {
		t.Fatalf("relecture : %q %v %v", l, ok, err)
	}
	if l, ok := e.svc.GroupLanguage(ctx, e.slug); !ok || l != "en" {
		t.Fatalf("port : %q %v", l, ok)
	}
	if _, ok := e.svc.GroupLanguage(ctx, "inconnu"); ok {
		t.Fatal("groupe inconnu : ok faux attendu")
	}
	if err := e.svc.SetDefaultLanguage(ctx, e.gid, ""); err != nil {
		t.Fatal(err)
	}
	if _, ok := e.svc.GroupLanguage(ctx, e.slug); ok {
		t.Fatal("valeur effacée attendue")
	}
}
