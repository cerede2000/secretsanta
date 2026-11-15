package groups

import (
	"context"
	"reflect"
	"regexp"
	"testing"
)

// F2.AC7 : le slug ne peut pas être modifié après la création. Aucun
// service de modification n'existe et la base refuse toute tentative. Les
// routes sont branchées par l'orchestrateur (internal/app) : aucune route de
// modification n'y est prévue, et ce module n'en expose pas.

var mutatingSlug = regexp.MustCompile(`(?i)(update|set|rename|change|edit|modify|move|patch|save|write)`)

func TestNoSlugModificationService(t *testing.T) {
	st := reflect.TypeOf(&Service{})
	allowed := map[string]bool{"Create": true, "VerifyAdminSecret": true, "ByID": true, "BySlug": true, "PublicPage": true}
	for i := 0; i < st.NumMethod(); i++ {
		m := st.Method(i)
		if !allowed[m.Name] {
			t.Fatalf("méthode inattendue %s : vérifier qu'elle ne modifie pas le slug puis l'ajouter ici", m.Name)
		}
		if mutatingSlug.MatchString(m.Name) {
			t.Fatalf("méthode de modification %s", m.Name)
		}
	}
}

func TestSlugUpdateRefusedByDatabase(t *testing.T) {
	e := newEnv(t)
	r := mustCreate(t, e, validInput("famille"))
	if _, err := e.db.Exec(`UPDATE groups SET slug = 'autre' WHERE id = ?`, r.Group.ID); err == nil {
		t.Fatal("la base accepte la modification du slug")
	}
	if _, err := e.db.Exec(`UPDATE groups SET slug = upper(slug) WHERE id = ?`, r.Group.ID); err == nil {
		t.Fatal("la base accepte la modification du slug")
	}
	// Réécrire la même valeur ou modifier une autre colonne reste possible.
	if _, err := e.db.Exec(`UPDATE groups SET slug = slug, name = 'Nouveau nom' WHERE id = ?`, r.Group.ID); err != nil {
		t.Fatalf("modification d'une autre colonne refusée : %v", err)
	}
	g, err := e.svc.ByID(context.Background(), r.Group.ID)
	if err != nil || g.Slug != "famille" || g.Name != "Nouveau nom" {
		t.Fatalf("%+v %v", g, err)
	}
}

func TestRecreatingSameSlugDoesNotOverwrite(t *testing.T) {
	e := newEnv(t)
	r := mustCreate(t, e, validInput("famille"))
	in := validInput("famille")
	in.Name = "Usurpateur"
	if _, err := e.svc.Create(context.Background(), in); err == nil {
		t.Fatal("recréation acceptée")
	}
	g, _ := e.svc.BySlug(context.Background(), "famille")
	if g.ID != r.Group.ID || g.Name != "Famille Martin" {
		t.Fatalf("groupe écrasé : %+v", g)
	}
	if _, err := e.svc.VerifyAdminSecret(context.Background(), "famille", r.AdminSecret); err != nil {
		t.Fatal("secret d'origine invalidé")
	}
}
