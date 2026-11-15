package letter

import (
	"strconv"
	"testing"
)

func TestEveryModificationEmitsVersionedEvent(t *testing.T) {
	e := newEnv(t)
	p := e.person(t, "Alice", "normal")
	ctx := t.Context()
	id, _ := e.svc.AddIdea(ctx, e.group, p, IdeaInput{Title: "a"})
	_ = e.svc.UpdateIdea(ctx, e.group, p, id, IdeaInput{Title: "b"})
	_, _ = e.svc.SetIntro(ctx, e.group, p, "intro")
	_ = e.svc.DeleteIdea(ctx, e.group, p, id)
	// Les refus n'émettent rien.
	_, _ = e.svc.AddIdea(ctx, e.group, p, IdeaInput{Title: ""})
	evs := e.events()
	if len(evs) != 4 {
		t.Fatalf("4 événements attendus, %d", len(evs))
	}
	for i, ev := range evs {
		if ev.GroupID != e.group || ev.Payload[PayloadVersion] != strconv.Itoa(i+1) {
			t.Fatalf("événement %d : %+v", i, ev)
		}
		if ev.Payload[PayloadKey] != "letter:"+ev.Payload[PayloadLetterID] || ev.Payload[PayloadAt] == "" {
			t.Fatalf("clé ou horodatage manquant : %+v", ev)
		}
	}
	if evs[0].Payload[PayloadIdeas] != strconv.FormatInt(id, 10) {
		t.Fatalf("idées : %v", evs[0].Payload)
	}
}
