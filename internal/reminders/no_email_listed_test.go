package reminders

import (
	"context"
	"testing"
	"time"

	"secretsanta/internal/participants"
)

// F7.AC8 : sans courriel, jamais de relance automatique ; listé pour remise manuelle.
func TestNoEmailNeverRemindedAndListed(t *testing.T) {
	e := newEnv(t, paris(2026, time.November, 10, 10, 0))
	j := paris(2026, time.November, 1, 10, 0)
	e.add(participants.Participant{Name: "Zoé", JoinedAt: j})
	b := e.add(participants.Participant{Name: "Bastien", JoinedAt: j})
	c := e.add(participants.Participant{Name: "Carole", JoinedAt: j})
	e.add(participants.Participant{Name: "Dario", Email: "dario@example.net", JoinedAt: j})
	e.facts.linkOpened[c] = true
	e.facts.written[c] = true // Carole est à jour : hors liste

	if n := e.tick(t); n != 1 { // Dario seul
		t.Fatalf("%d relance(s), 1 attendue", n)
	}
	for _, it := range e.q.items {
		if it.To == "" || it.ParticipantID == b {
			t.Fatalf("relance à une personne sans courriel : %+v", it)
		}
	}
	list, err := e.eng.ManualDeliveryList(context.Background(), e.gid)
	must(t, err)
	if len(list) != 2 || list[0].Name != "Bastien" || list[1].Name != "Zoé" {
		t.Fatalf("liste de remise : %+v", list)
	}
	if !list[0].LinkNotOpened || !list[0].LetterNotWritten {
		t.Fatalf("détail : %+v", list[0])
	}

	// La relance manuelle d'une personne sans courriel le signale, sans rien envoyer.
	res, err := e.eng.Remind(context.Background(), e.gid, KindLink, []int64{b})
	must(t, err)
	if res.Count(OutcomeNoEmail) != 1 {
		t.Fatalf("%+v", res)
	}
}
