package letter

import (
	"sort"
	"strings"
	"testing"
	"time"

	"secretsanta/internal/shared/clock"
	"secretsanta/internal/shared/ports"
)

// coalescer est un récepteur de test conforme au contrat de regroupement :
// un message par clé, envoyé CoalesceQuiet après la dernière modification et au
// plus tard CoalesceCap après la première.
type coalescer struct {
	clock *clock.Fake
	state map[string]*pendingMsg
	Sent  []sentMsg
}

type pendingMsg struct {
	first, last time.Time
	version     string
	ideas       map[string]bool
}

type sentMsg struct {
	Key     string
	Version string
	Ideas   []string
	Payload map[string]string
}

func (c *coalescer) receive(e ports.Event) {
	k := e.Payload[PayloadKey]
	p := c.state[k]
	if p == nil {
		p = &pendingMsg{first: c.clock.Now(), ideas: map[string]bool{}}
		c.state[k] = p
	}
	p.last = c.clock.Now()
	p.version = e.Payload[PayloadVersion]
	for _, id := range strings.Split(e.Payload[PayloadIdeas], ",") {
		if id != "" {
			p.ideas[id] = true
		}
	}
}

func (c *coalescer) tick() {
	now := c.clock.Now()
	for k, p := range c.state {
		if !now.Before(p.last.Add(CoalesceQuiet)) || !now.Before(p.first.Add(CoalesceCap)) {
			var ids []string
			for id := range p.ideas {
				ids = append(ids, id)
			}
			sort.Strings(ids)
			c.Sent = append(c.Sent, sentMsg{Key: k, Version: p.version, Ideas: ids})
			delete(c.state, k)
		}
	}
}

func TestFiveModificationsCoalesceIntoOneMessage(t *testing.T) {
	e := newEnv(t)
	p := e.person(t, "Alice", "normal")
	e.person(t, "Zorglub", "normal")
	ctx := t.Context()
	rc := &coalescer{clock: e.clock, state: map[string]*pendingMsg{}}
	e.bus.Subscribe(EventUpdated, func(_ context_, ev ports.Event) error { rc.receive(ev); return nil })

	var first int64
	for i := 0; i < 5; i++ {
		var err error
		if i == 0 {
			first, err = e.svc.AddIdea(ctx, e.group, p, IdeaInput{Title: "a"})
		} else if i < 3 {
			_, err = e.svc.AddIdea(ctx, e.group, p, IdeaInput{Title: "n"})
		} else {
			err = e.svc.UpdateIdea(ctx, e.group, p, first, IdeaInput{Title: "a2"})
		}
		if err != nil {
			t.Fatal(err)
		}
		e.clock.Advance(1 * time.Minute)
		rc.tick()
	}
	if len(rc.Sent) != 0 {
		t.Fatal("rien ne doit partir avant la fin de l'attente")
	}
	e.clock.Advance(CoalesceQuiet)
	rc.tick()
	if len(rc.Sent) != 1 {
		t.Fatalf("un seul message attendu, %d", len(rc.Sent))
	}
	m := rc.Sent[0]
	if m.Version != "5" || len(m.Ideas) != 3 || m.Key == "" {
		t.Fatalf("%+v", m)
	}
	for _, ev := range e.events() {
		for _, v := range ev.Payload {
			if strings.Contains(v, "Zorglub") {
				t.Fatal("identité")
			}
		}
	}
	// L'ensemble cumulé de l'événement suivant repart de zéro après MarkNotified.
	lid := e.events()[0].Payload[PayloadLetterID]
	var n int64
	_, _ = fmtSscan(lid, &n)
	if err := e.svc.MarkNotified(ctx, n); err != nil {
		t.Fatal(err)
	}
	if err := e.svc.UpdateIdea(ctx, e.group, p, first, IdeaInput{Title: "a3"}); err != nil {
		t.Fatal(err)
	}
	evs := e.events()
	if got := evs[len(evs)-1].Payload[PayloadIdeas]; got != lid2s(first) {
		t.Fatalf("cumul remis à zéro attendu, obtenu %q", got)
	}
}

func TestCoalescingCapAfter60Minutes(t *testing.T) {
	e := newEnv(t)
	p := e.person(t, "Alice", "normal")
	rc := &coalescer{clock: e.clock, state: map[string]*pendingMsg{}}
	e.bus.Subscribe(EventUpdated, func(_ context_, ev ports.Event) error { rc.receive(ev); return nil })
	id, _ := e.svc.AddIdea(t.Context(), e.group, p, IdeaInput{Title: "a"})
	for i := 0; i < 8; i++ { // une modification toutes les 9 minutes
		e.clock.Advance(9 * time.Minute)
		_ = e.svc.UpdateIdea(t.Context(), e.group, p, id, IdeaInput{Title: "b"})
		rc.tick()
	}
	if len(rc.Sent) != 1 {
		t.Fatalf("le plafond de 60 min impose un envoi : %d", len(rc.Sent))
	}
}
