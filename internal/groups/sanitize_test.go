package groups

import (
	"context"
	"strings"
	"testing"
	"unicode/utf8"

	"secretsanta/internal/shared/errs"
)

// F2.AC5 : entrées trop longues ou avec caractères de contrôle refusées ou
// nettoyées à la création de groupe.

func TestControlCharactersCleaned(t *testing.T) {
	e := newEnv(t)
	in := validInput("famille")
	in.Name = "  Fa\x00mille\x07 \t Martin​‮ "
	in.GiftInstructions = "Ligne 1\x1b[31m\r\nLigne\u0000 2\n\n\n\nFin"
	r := mustCreate(t, e, in)
	if r.Group.Name != "Famille Martin" {
		t.Fatalf("nom %q", r.Group.Name)
	}
	if strings.ContainsAny(r.Group.GiftInstructions, "\x00\x1b\r") {
		t.Fatalf("consigne non nettoyée : %q", r.Group.GiftInstructions)
	}
	if r.Group.GiftInstructions != "Ligne 1[31m\nLigne 2\n\nFin" {
		t.Fatalf("consigne %q", r.Group.GiftInstructions)
	}
	// Ce qui est relu en base est la version nettoyée.
	g, err := e.svc.BySlug(context.Background(), "famille")
	if err != nil || g.Name != r.Group.Name || g.GiftInstructions != r.Group.GiftInstructions {
		t.Fatalf("%+v %v", g, err)
	}
}

func TestTooLongInputsRefused(t *testing.T) {
	e := newEnv(t)
	cases := map[string]func(*CreateInput){
		"nom":      func(in *CreateInput) { in.Name = strings.Repeat("é", NameMaxLen+1) },
		"consigne": func(in *CreateInput) { in.GiftInstructions = strings.Repeat("a", GiftInstructionsMax+1) },
		"slug":     func(in *CreateInput) { in.Slug = strings.Repeat("a", 10_000) },
		"budget":   func(in *CreateInput) { in.Budget = strings.Repeat("1", 50) },
		"jour J":   func(in *CreateInput) { in.EventDate = strings.Repeat("2026-12-24", 3) },
		"nom vide": func(in *CreateInput) { in.Name = " \x00\t " },
	}
	for name, mod := range cases {
		in := validInput("famille")
		mod(&in)
		_, err := e.svc.Create(context.Background(), in)
		if err == nil {
			t.Fatalf("%s : saisie acceptée", name)
		}
		wantFrench(t, err, errs.KindValidation)
	}
	if countGroups(t, e.db) != 0 {
		t.Fatal("saisie trop longue enregistrée")
	}
}

func TestMaxLengthsCountCharactersNotBytes(t *testing.T) {
	e := newEnv(t)
	in := validInput("famille")
	in.Name = strings.Repeat("é", NameMaxLen)
	in.GiftInstructions = strings.Repeat("ü", GiftInstructionsMax)
	r := mustCreate(t, e, in)
	if utf8.RuneCountInString(r.Group.Name) != NameMaxLen {
		t.Fatal(r.Group.Name)
	}
}

func TestOversizedInstancePasswordIsJustWrong(t *testing.T) {
	e := newEnv(t)
	in := validInput("famille")
	in.InstancePassword = testPassword + strings.Repeat("x", 1<<20)
	_, err := e.svc.Create(context.Background(), in)
	wantFrench(t, err, errs.KindUnauthorized)
}

func TestEventDateValidated(t *testing.T) {
	e := newEnv(t)
	for _, bad := range []string{"24/12/2026", "2026-13-01", "demain", "2026-02-30"} {
		in := validInput("famille")
		in.EventDate = bad
		_, err := e.svc.Create(context.Background(), in)
		wantFrench(t, err, errs.KindValidation)
	}
	in := validInput("famille")
	in.EventDate = " 2026-12-24 "
	r := mustCreate(t, e, in)
	if r.Group.EventDate != "2026-12-24" {
		t.Fatal(r.Group.EventDate)
	}
}
