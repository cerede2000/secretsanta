package mailboxpage

import (
	"testing"

	"secretsanta/internal/participants"
)

// La boîte du tireur dit, pour chaque lettre reçue écrite : « À lire » avant sa
// lecture, « Rien de nouveau » après, « Nouvelle idée » quand la lettre a changé
// depuis ; en anglais, les puces sont traduites.
func TestReceivedLetterChips(t *testing.T) {
	e := newEnv(t)
	s := newScene(t, e, "puces-lettre")
	giver := s.more[0]
	targets := e.discover(t, s.gid, giver, 1)
	tg := targets[1]
	if _, err := e.letters.SetIntro(ctx, s.gid, tg.ReceiverID, "Cher Père Noël"); err != nil {
		t.Fatal(err)
	}
	e.login(t, s.gid, giver)
	box := func() string { return e.do("GET", "/"+s.slug+"/boite", "", nil).Body.String() }

	body := box()
	mustContain(t, body, "À lire")
	mustNotContain(t, body, "Nouvelle idée")
	mustNotContain(t, body, "Rien de nouveau")

	if _, err := e.read.Read(ctx, participants.ParticipantActor(s.gid, giver), s.gid, tg.PairID); err != nil {
		t.Fatal(err)
	}
	body = box()
	mustContain(t, body, "Rien de nouveau")
	mustNotContain(t, body, "À lire")

	if _, err := e.letters.AddIdea(ctx, s.gid, tg.ReceiverID, ideaInput("Une écharpe")); err != nil {
		t.Fatal(err)
	}
	body = box()
	mustContain(t, body, "Nouvelle idée")
	mustNotContain(t, body, "Rien de nouveau")
	mustNotContain(t, body, "Une écharpe") // jamais le contenu dans la boîte

	en := e.do("GET", "/"+s.slug+"/boite?lang=en", "en", nil).Body.String()
	mustContain(t, en, "New idea")
	mustNotContain(t, en, "mailbox.received.state")
}
