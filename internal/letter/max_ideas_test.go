package letter

import (
	"fmt"
	"testing"

	"secretsanta/internal/shared/errs"
)

func TestSixthIdeaRefused(t *testing.T) {
	e := newEnv(t)
	p := e.person(t, "Alice", "normal")
	ctx := t.Context()
	for i := 1; i <= 5; i++ {
		if _, err := e.svc.AddIdea(ctx, e.group, p, IdeaInput{Title: fmt.Sprint("Idée ", i)}); err != nil {
			t.Fatalf("idée %d : %v", i, err)
		}
	}
	_, err := e.svc.AddIdea(ctx, e.group, p, IdeaInput{Title: "Sixième"})
	wantKind(t, err, errs.KindValidation)
	if err.Error() != msgMaxIdeas {
		t.Fatalf("message français attendu, obtenu %q", err.Error())
	}
	l, _ := e.svc.Get(ctx, e.group, p)
	if len(l.Ideas) != 5 || l.Version != 5 {
		t.Fatalf("5 idées et version 5 attendues : %d, v%d", len(l.Ideas), l.Version)
	}
	// Le plafond tient aussi dans la base.
	if _, err := e.db.Exec(`INSERT INTO letter_ideas (letter_id, position, title) VALUES (?, 9, 'x')`, l.ID); err == nil {
		t.Fatal("la base doit refuser la 6e idée")
	}
	// Après suppression, on peut en ajouter une.
	if err := e.svc.DeleteIdea(ctx, e.group, p, l.Ideas[0].ID); err != nil {
		t.Fatal(err)
	}
	if _, err := e.svc.AddIdea(ctx, e.group, p, IdeaInput{Title: "Nouvelle"}); err != nil {
		t.Fatal(err)
	}
}
