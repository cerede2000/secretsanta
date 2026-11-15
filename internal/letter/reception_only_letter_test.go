package letter

import (
	"fmt"
	"testing"

	"secretsanta/internal/shared/errs"
)

func TestReceptionOnlyWritesLikeOthers(t *testing.T) {
	e := newEnv(t)
	normal := e.person(t, "Alice", "normal")
	recv := e.person(t, "Bruno", "reception_seule")
	ctx := t.Context()
	for _, p := range []int64{normal, recv} {
		for i := 1; i <= 5; i++ {
			if _, err := e.svc.AddIdea(ctx, e.group, p, IdeaInput{Title: fmt.Sprint("i", i), URL: "https://exemple.fr"}); err != nil {
				t.Fatal(err)
			}
		}
		_, err := e.svc.AddIdea(ctx, e.group, p, IdeaInput{Title: "6"})
		wantKind(t, err, errs.KindValidation)
		if _, err := e.svc.AddIdea(ctx, e.group, p, IdeaInput{Title: "x", URL: "javascript:1"}); err == nil {
			t.Fatal("lien refusé")
		}
	}
	// « Après le tirage » : le rôle reste, le service ne dépend d'aucun état de tirage.
	l, _ := e.svc.Get(ctx, e.group, recv)
	if err := e.svc.UpdateIdea(ctx, e.group, recv, l.Ideas[0].ID, IdeaInput{Title: "modifiée"}); err != nil {
		t.Fatal(err)
	}
	if _, err := e.svc.SetIntro(ctx, e.group, recv, "Salut"); err != nil {
		t.Fatal(err)
	}
	if err := e.svc.DeleteIdea(ctx, e.group, recv, l.Ideas[1].ID); err != nil {
		t.Fatal(err)
	}
	// Mêmes événements que pour les autres : 5 ajouts + 3 modifications chacun, sans tireur.
	counts := map[string]int{}
	for _, ev := range e.events() {
		counts[ev.Payload[PayloadRecipientID]]++
		if len(ev.Payload) != 6 {
			t.Fatalf("charge utile inattendue : %v", ev.Payload)
		}
	}
	if counts[lid2s(recv)] != 8 || counts[lid2s(normal)] != 5 {
		t.Fatalf("%v", counts)
	}
	// Une lettre n'appartient qu'à son auteur : l'autre ne modifie pas ses idées.
	if err := e.svc.UpdateIdea(ctx, e.group, normal, l.Ideas[2].ID, IdeaInput{Title: "vol"}); !errs.Is(err, errs.KindNotFound) {
		t.Fatalf("idée d'un autre : %v", err)
	}
}
