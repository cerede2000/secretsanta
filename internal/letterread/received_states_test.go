package letterread

import (
	"testing"

	"secretsanta/internal/letter"
	"secretsanta/internal/participants"
)

// ReceivedStates dit au tireur, pour chaque cible dont la lettre est écrite :
// jamais lue (unread), nouvelle version depuis sa lecture (new), ou rien de
// nouveau (seen). Une lettre jamais écrite n'est pas listée, et seul le tireur
// de la paire voit son état.
func TestReceivedStates(t *testing.T) {
	e := newEnv(t)
	gid := e.group(t, "etats")
	ids := e.addN(t, gid, "Amandine", 4, participants.RoleNormal)
	e.write(t, gid, ids[:3]...) // ids[3] n'écrit pas
	e.mustDraw(t, gid)
	e.openReveal()
	e.discoverAll(t, gid)

	tire := ids[0]
	p := e.pairOf(t, gid, tire)
	state := func(giver, pair int64) (string, bool) {
		t.Helper()
		m, err := e.svc.ReceivedStates(ctx, who(gid, giver), gid)
		if err != nil {
			t.Fatal(err)
		}
		s, ok := m[pair]
		return s, ok
	}
	if s, ok := state(p.Giver, p.ID); !ok || s != ReceivedUnread {
		t.Fatalf("avant lecture : %q %v", s, ok)
	}
	e.mustRead(t, gid, p.Giver, p.ID, tire)
	if s, _ := state(p.Giver, p.ID); s != ReceivedSeen {
		t.Fatalf("après lecture : %q", s)
	}
	if _, err := e.letters.AddIdea(ctx, gid, tire, letter.IdeaInput{Title: "Encore"}); err != nil {
		t.Fatal(err)
	}
	if s, _ := state(p.Giver, p.ID); s != ReceivedNew {
		t.Fatalf("après une idée ajoutée : %q", s)
	}
	e.mustRead(t, gid, p.Giver, p.ID, tire)
	if s, _ := state(p.Giver, p.ID); s != ReceivedSeen {
		t.Fatalf("après relecture : %q", s)
	}
	// Lettre jamais écrite : absente de la liste.
	lp := e.pairOf(t, gid, ids[3])
	if s, ok := state(lp.Giver, lp.ID); ok {
		t.Fatalf("lettre non écrite listée : %q", s)
	}
	// Un autre participant ne voit pas la paire ; l'administrateur n'a pas cette vue.
	for _, o := range ids {
		if o == p.Giver {
			continue
		}
		if _, ok := state(o, p.ID); ok {
			t.Fatalf("la paire %d est visible de %d", p.ID, o)
		}
	}
	if _, err := e.svc.ReceivedStates(ctx, admin(gid), gid); err == nil {
		t.Fatal("la vue du tireur n'est pas servie à l'administrateur")
	}
}
