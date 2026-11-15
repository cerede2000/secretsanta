package reminders

import (
	"context"
	"testing"
	"time"

	"secretsanta/internal/participants"
)

// F7.AC7 : relance manuelle d'une personne ou d'une liste, messages en file,
// personnes déjà relancées aujourd'hui signalées.
func TestManualRemindListQueuesAndFlagsAlreadyToday(t *testing.T) {
	e := newEnv(t, paris(2026, time.November, 5, 10, 0))
	j := paris(2026, time.November, 1, 10, 0)
	a := e.add(participants.Participant{Name: "Anna", Email: "anna@example.net", JoinedAt: j})
	b := e.add(participants.Participant{Name: "Bruno", Email: "bruno@example.net", JoinedAt: j})
	ctx := context.Background()

	res, err := e.eng.Remind(ctx, e.gid, KindLink, []int64{a})
	must(t, err)
	if res.Count(OutcomeQueued) != 1 || len(e.q.items) != 1 {
		t.Fatalf("une personne : %+v, %d en file", res, len(e.q.items))
	}
	it := e.q.items[0]
	if it.To != "anna@example.net" || it.Type != "relances" || it.Essential || it.UnsubscribeURL == "" {
		t.Fatalf("élément de file inattendu : %+v", it)
	}

	// Liste (avec doublon) : Anna est déjà relancée aujourd'hui, Bruno part.
	res, err = e.eng.Remind(ctx, e.gid, KindLink, []int64{a, b, b})
	must(t, err)
	if got := res.IDs(OutcomeAlreadyToday); len(got) != 1 || got[0] != a {
		t.Fatalf("déjà relancées : %v", got)
	}
	if got := res.IDs(OutcomeQueued); len(got) != 1 || got[0] != b || len(res.Items) != 2 {
		t.Fatalf("mises en file : %v (%d lignes)", got, len(res.Items))
	}
	if len(e.q.items) != 2 {
		t.Fatalf("%d courriels en file, 2 attendus", len(e.q.items))
	}

	// Le lendemain, Anna peut être relancée de nouveau.
	e.clk.Set(paris(2026, time.November, 6, 10, 0))
	res, err = e.eng.Remind(ctx, e.gid, KindLink, []int64{a})
	must(t, err)
	if res.Count(OutcomeQueued) != 1 {
		t.Fatalf("lendemain : %+v", res)
	}
}

// Le garde-fou du jour est partagé avec l'automatique et entre les types.
func TestManualSharesDailyGuardWithAutomaticAndOtherTypes(t *testing.T) {
	e := newEnv(t, paris(2026, time.November, 5, 10, 0))
	a := e.add(participants.Participant{Name: "Anna", Email: "anna@example.net", JoinedAt: paris(2026, time.November, 1, 10, 0)})
	if n := e.tick(t); n != 1 {
		t.Fatalf("automatique : %d", n)
	}
	res, err := e.eng.Remind(context.Background(), e.gid, KindLetter, []int64{a})
	must(t, err)
	if res.Count(OutcomeAlreadyToday) != 1 || len(e.q.items) != 1 {
		t.Fatalf("%+v, %d en file", res, len(e.q.items))
	}
}

func TestManualUnknownPersonAndBadKind(t *testing.T) {
	e := newEnv(t, paris(2026, time.November, 5, 10, 0))
	res, err := e.eng.Remind(context.Background(), e.gid, KindLink, []int64{999})
	must(t, err)
	if res.Count(OutcomeUnknown) != 1 || len(e.q.items) != 0 {
		t.Fatalf("%+v", res)
	}
	if _, err := e.eng.Remind(context.Background(), e.gid, Kind("x"), nil); err == nil {
		t.Fatal("type inconnu accepté")
	}
}
