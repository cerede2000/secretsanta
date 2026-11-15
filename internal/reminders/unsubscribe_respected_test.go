package reminders

import (
	"context"
	"testing"
	"time"

	"secretsanta/internal/mail/templates"
	"secretsanta/internal/mailunsub"
	"secretsanta/internal/participants"
)

// F7.AC10 : désinscription par type respectée ; les essentiels restent envoyés.
func TestUnsubscribedTypeRespected(t *testing.T) {
	e := newEnv(t, paris(2026, time.November, 5, 10, 0))
	j := paris(2026, time.November, 1, 10, 0)
	a := e.add(participants.Participant{Name: "Anna", Email: "anna@example.net", JoinedAt: j})
	b := e.add(participants.Participant{Name: "Bruno", Email: "bruno@example.net", JoinedAt: j})
	ctx := context.Background()
	must(t, e.unsub.Unsubscribe(ctx, e.gid, a, mailunsub.TypeRelances))

	// Automatique : seule Bruno est relancé.
	if n := e.tick(t); n != 1 || len(e.to(a)) != 0 || len(e.to(b)) != 1 {
		t.Fatalf("auto : %d relance(s), Anna %d, Bruno %d", n, len(e.to(a)), len(e.to(b)))
	}
	// Manuel : signalée comme désinscrite.
	res, err := e.eng.Remind(ctx, e.gid, KindLetter, []int64{a})
	must(t, err)
	if res.Count(OutcomeUnsubscribed) != 1 || len(e.to(a)) != 0 {
		t.Fatalf("manuel : %+v", res)
	}
	// Une désinscription refusée n'a pas consommé le courriel du jour.
	if ok, _ := e.rules.SentToday(ctx, e.gid, a); ok {
		t.Fatal("la réservation du jour a été consommée par une personne désinscrite")
	}

	// Nouvelle idée : coupée si désinscrite de ce type, indépendante du type relances.
	if ok, err := e.eng.MayNotify(ctx, templates.LetterUpdated, a); err != nil || !ok {
		t.Fatalf("nouvelle_idee non désinscrit : %v %v", ok, err)
	}
	must(t, e.unsub.Unsubscribe(ctx, e.gid, a, mailunsub.TypeNouvelleIdee))
	if ok, _ := e.eng.MayNotify(ctx, templates.LetterUpdated, a); ok {
		t.Fatal("notification de nouvelle idée envoyée à une personne désinscrite")
	}
	// Les essentiels partent toujours.
	for _, k := range []templates.Kind{templates.Verification, templates.MagicLink, templates.Welcome, templates.DrawAdjusted} {
		if ok, err := e.eng.MayNotify(ctx, k, a); err != nil || !ok {
			t.Fatalf("mail essentiel %s bloqué", k)
		}
	}
}
