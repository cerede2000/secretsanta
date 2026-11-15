package givernotify

import (
	"testing"
	"time"

	"secretsanta/internal/mailunsub"
)

const day = 24 * time.Hour

// F7b.AC1 : la relance part pour une lettre non ouverte, après le délai de
// F7, et s'arrête à l'ouverture.
func TestGiverReminderStartsOnUnopenedLetter(t *testing.T) {
	e := newEnv(t, "fr")
	gid, _ := e.singleGroup(t, "famille")
	giver, ps := e.firstGiverWith(t, gid, 1)
	e.edit(t, gid, ps[0].Receiver, 1)
	if e.remind(t) != 0 {
		t.Fatal("pas de relance le jour même")
	}
	e.clock.Advance(3*day - time.Hour)
	if e.remind(t) != 0 {
		t.Fatal("pas de relance avant le délai")
	}
	e.clock.Advance(time.Hour)
	if n := e.remind(t); n < 1 {
		t.Fatal("relance attendue après 3 jours")
	}
	got := e.q.to(giver, mailunsub.TypeRelances)
	if len(got) != 1 {
		t.Fatalf("%d relances pour le tireur", len(got))
	}
	noOtherName(t, e, gid, giver, got[0])
}

func TestGiverReminderStopsOnOpen(t *testing.T) {
	e := newEnv(t, "fr")
	gid, _ := e.singleGroup(t, "famille")
	giver, ps := e.firstGiverWith(t, gid, 1)
	e.edit(t, gid, ps[0].Receiver, 1)
	e.open(t, gid, ps[0])
	e.clock.Advance(3 * day)
	e.remind(t)
	if n := len(e.q.to(giver, mailunsub.TypeRelances)); n != 0 {
		t.Fatalf("%d relances malgré l'ouverture", n)
	}
	// Une nouvelle modification rend la lettre « non ouverte » : la relance repart.
	e.edit(t, gid, ps[0].Receiver, 2)
	e.clock.Advance(3 * day)
	e.remind(t)
	if n := len(e.q.to(giver, mailunsub.TypeRelances)); n != 1 {
		t.Fatalf("%d relances après modification, 1 attendue", n)
	}
	// Elle s'arrête dès que la nouvelle version est ouverte.
	e.open(t, gid, ps[0])
	e.clock.Advance(10 * day)
	e.remind(t)
	if n := len(e.q.to(giver, mailunsub.TypeRelances)); n != 1 {
		t.Fatalf("%d relances après ouverture, 1 attendue", n)
	}
}

// Respect des désinscriptions par type.
func TestGiverMailsRespectUnsubscribeByType(t *testing.T) {
	e := newEnv(t, "fr")
	gid, _ := e.singleGroup(t, "famille")
	giver, ps := e.firstGiverWith(t, gid, 1)
	if err := e.unsub.Unsubscribe(ctx, gid, giver, mailunsub.TypeNouvelleIdee); err != nil {
		t.Fatal(err)
	}
	e.edit(t, gid, ps[0].Receiver, 1)
	if n := len(e.q.to(giver, mailunsub.TypeNouvelleIdee)); n != 0 {
		t.Fatal("désinscrit de nouvelle_idee : aucun courriel")
	}
	e.clock.Advance(3 * day)
	e.remind(t)
	if n := len(e.q.to(giver, mailunsub.TypeRelances)); n != 1 {
		t.Fatal("la désinscription nouvelle_idee ne coupe pas la relance")
	}
	if err := e.unsub.Unsubscribe(ctx, gid, giver, mailunsub.TypeRelances); err != nil {
		t.Fatal(err)
	}
	e.clock.Advance(10 * day)
	e.remind(t)
	if n := len(e.q.to(giver, mailunsub.TypeRelances)); n != 1 {
		t.Fatal("désinscrit de relances : plus de relance")
	}
}

// Un seul courriel de relance par jour (garde-fou de F7) et hors fenêtre rien.
func TestGiverReminderRespectsWindowAndDailyGuard(t *testing.T) {
	e := newEnv(t, "fr")
	gid, _ := e.singleGroup(t, "famille")
	giver, ps := e.firstGiverWith(t, gid, 1)
	e.edit(t, gid, ps[0].Receiver, 1)
	e.clock.Advance(3*day + 12*time.Hour) // 23 h à Paris : hors fenêtre
	if e.remind(t) != 0 {
		t.Fatal("aucune relance hors de la fenêtre d'envoi")
	}
	e.clock.Advance(10 * time.Hour) // 09 h à Paris le lendemain
	if e.remind(t) < 1 {
		t.Fatal("relance attendue dans la fenêtre")
	}
	if e.remind(t) != 0 || len(e.q.to(giver, mailunsub.TypeRelances)) != 1 {
		t.Fatal("un seul courriel de relance par jour")
	}
}
