package groups

import (
	"context"
	"strings"
	"testing"

	"secretsanta/internal/shared/errs"
)

// F2.AC2 : slugs réservés, invalides ou déjà pris refusés avec un message
// français.

func TestReservedSlugsRefused(t *testing.T) {
	e := newEnv(t)
	required := []string{"admin", "api", "assets", "static", "health", "creer"}
	for _, s := range required {
		if !IsReservedSlug(s) {
			t.Fatalf("%q devrait être réservé", s)
		}
	}
	list := ReservedSlugs()
	if len(list) < len(required) {
		t.Fatalf("liste réservée trop courte : %v", list)
	}
	for i, s := range list {
		if !isSlugSyntax(s) {
			t.Fatalf("slug réservé %q non conforme à la syntaxe : il ne protégerait rien", s)
		}
		in := validInput(s)
		in.ClientKey = "c" + s
		_, err := e.svc.Create(context.Background(), in)
		wantFrench(t, err, errs.KindValidation)
		// Un nom réservé plus court que le minimum est déjà refusé par la
		// longueur ; il reste listé au cas où la borne baisserait.
		if len(s) >= SlugMinLen && errs.UserMessage(err) != msgSlugReserved {
			t.Fatalf("%d %q : %v", i, s, err)
		}
	}
	// ReservedSlugs renvoie une copie.
	list[0] = "zzz-modifie"
	if IsReservedSlug("zzz-modifie") {
		t.Fatal("la liste réservée est modifiable de l'extérieur")
	}
	if countGroups(t, e.db) != 0 {
		t.Fatal("un slug réservé a été enregistré")
	}
}

func TestInvalidSlugsRefused(t *testing.T) {
	e := newEnv(t)
	invalid := []string{
		"", "   ", "ab", strings.Repeat("a", SlugMaxLen+1),
		"Famille", "famille martin", "famille_martin", "famille.martin", "-famille", "famille-",
		"famille--martin", "noël", "famille/martin", "../etc", "famille\x00", "famille​",
		"%2e%2e", "famille?x=1", "famille#a",
	}
	for _, s := range invalid {
		_, err := ValidateSlug(s)
		wantFrench(t, err, errs.KindValidation)
		_, err = e.svc.Create(context.Background(), validInput(s))
		wantFrench(t, err, errs.KindValidation)
	}
	if countGroups(t, e.db) != 0 {
		t.Fatal("un slug invalide a été enregistré")
	}
}

func TestValidSlugsAccepted(t *testing.T) {
	e := newEnv(t)
	for _, s := range []string{"abc", "famille-martin", "noel-2026", "2026", strings.Repeat("a", SlugMaxLen), "  bords  "} {
		r := mustCreate(t, e, validInput(s))
		if r.Group.Slug != strings.TrimSpace(s) {
			t.Fatalf("slug %q enregistré comme %q", s, r.Group.Slug)
		}
	}
}

func TestTakenSlugRefused(t *testing.T) {
	e := newEnv(t)
	mustCreate(t, e, validInput("famille"))
	_, err := e.svc.Create(context.Background(), validInput("famille"))
	wantFrench(t, err, errs.KindConflict)
	if errs.UserMessage(err) != msgSlugTaken {
		t.Fatalf("%v", err)
	}
	if countGroups(t, e.db) != 1 {
		t.Fatal("doublon enregistré")
	}
}

func TestSchemaRefusesInvalidSlugDirectly(t *testing.T) {
	e := newEnv(t)
	for _, s := range []string{"ab", "Famille", "a b", "famille_x"} {
		_, err := e.db.Exec(`INSERT INTO groups (slug, name, admin_secret_hash, created_at) VALUES (?, 'x', zeroblob(32), 'now')`, s)
		if err == nil {
			t.Fatalf("la base accepte le slug %q", s)
		}
	}
}
