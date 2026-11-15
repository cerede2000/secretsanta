package letter

import "testing"

func TestWriteAndEditBeforeDraw(t *testing.T) {
	e := newEnv(t) // aucun tirage n'existe : le service n'en dépend pas
	p := e.person(t, "Alice", "normal")
	ctx := t.Context()
	l, err := e.svc.Get(ctx, e.group, p)
	if err != nil || l.Version != 0 || len(l.Ideas) != 0 {
		t.Fatalf("lettre vide attendue : %+v %v", l, err)
	}
	if _, err := e.svc.SetIntro(ctx, e.group, p, "Bonjour"); err != nil {
		t.Fatal(err)
	}
	id, err := e.svc.AddIdea(ctx, e.group, p, IdeaInput{Title: "Livre"})
	if err != nil {
		t.Fatal(err)
	}
	if err := e.svc.UpdateIdea(ctx, e.group, p, id, IdeaInput{Title: "Livre de cuisine", Text: "poche"}); err != nil {
		t.Fatal(err)
	}
	l, _ = e.svc.Get(ctx, e.group, p)
	if l.Version != 3 || l.Intro != "Bonjour" || len(l.Ideas) != 1 || l.Ideas[0].Title != "Livre de cuisine" {
		t.Fatalf("%+v", l)
	}
	if err := e.svc.DeleteIdea(ctx, e.group, p, id); err != nil {
		t.Fatal(err)
	}
	if l, _ = e.svc.Get(ctx, e.group, p); l.Version != 4 || len(l.Ideas) != 0 {
		t.Fatalf("%+v", l)
	}
	// Une personne inconnue du groupe ne peut pas écrire.
	if _, err := e.svc.AddIdea(ctx, e.group, 9999, IdeaInput{Title: "x"}); err == nil {
		t.Fatal("participant inconnu refusé")
	}
}
