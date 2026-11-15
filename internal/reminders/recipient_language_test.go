package reminders

import (
	"regexp"
	"testing"
	"time"

	"secretsanta/internal/participants"
)

// F7.AC14 : langue du destinataire, sinon du groupe, jamais deux langues dans un message.
func TestReminderLanguageRecipientThenGroup(t *testing.T) {
	e := newEnv(t, paris(2026, time.November, 5, 10, 0))
	g := e.dir.groups[e.gid]
	g.Lang = "en"
	e.dir.groups[e.gid] = g
	j := paris(2026, time.November, 1, 10, 0)
	fr := e.add(participants.Participant{Name: "Léa", Email: "lea@example.net", Locale: "fr", JoinedAt: j})
	grp := e.add(participants.Participant{Name: "Sam", Email: "sam@example.net", JoinedAt: j}) // aucune langue : celle du groupe (en)
	if n := e.tick(t); n != 2 {
		t.Fatalf("%d relances", n)
	}
	frWords := regexp.MustCompile(`(?i)\b(bonjour|rappel|ton|tu|ta|lien|lettre)\b`)
	enWords := regexp.MustCompile(`(?i)\b(hello|hi|reminder|your|link|letter)\b`)
	check := func(pid int64, wantFR bool) {
		items := e.to(pid)
		if len(items) != 1 {
			t.Fatalf("%d courriels", len(items))
		}
		all := items[0].Subject + " " + items[0].Text
		f, en := frWords.MatchString(all), enWords.MatchString(all)
		if wantFR && (!f || en) {
			t.Errorf("attendu français pur : %q", all)
		}
		if !wantFR && (!en || f) {
			t.Errorf("attendu anglais pur : %q", all)
		}
	}
	check(fr, true)
	check(grp, false)
}
