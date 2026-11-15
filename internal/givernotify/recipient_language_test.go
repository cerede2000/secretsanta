package givernotify

import (
	"regexp"
	"testing"
	"time"

	"secretsanta/internal/mailunsub"
	"secretsanta/internal/participants"
)

var (
	frWords = regexp.MustCompile(`(?i)\b(bonjour|lettre|ton|tu|ta|lien|échange|idées?)\b`)
	enWords = regexp.MustCompile(`(?i)\b(hello|hi|letter|your|link|exchange|ideas?)\b`)
)

func checkLang(t *testing.T, subject, text string, wantFR bool) {
	t.Helper()
	all := subject + " " + text
	f, en := frWords.MatchString(all), enWords.MatchString(all)
	if wantFR && (!f || en) {
		t.Errorf("français pur attendu : %q", all)
	}
	if !wantFR && (!en || f) {
		t.Errorf("anglais pur attendu : %q", all)
	}
}

// F7b.AC6 : langue du destinataire, sinon du groupe, jamais deux langues.
func TestMailsInRecipientThenGroupLanguage(t *testing.T) {
	e := newEnv(t, "en") // langue du groupe : anglais
	gid := e.group(t, "famille")
	frGiver := e.add(t, gid, "Lea", "fr", participants.RoleNormal)
	var all []int64
	all = append(all, frGiver)
	all = append(all, e.addN(t, gid, "Mederic", 5, participants.RoleNormal)...)
	e.write(t, gid, all...)
	e.mustDraw(t, gid)
	e.clock.Set(e.revealA)

	givers := e.giversPairs(t, gid)
	for giver, ps := range givers {
		e.edit(t, gid, ps[0].Receiver, 1)
		_ = giver
	}
	e.clock.Advance(3 * day)
	e.remind(t)
	for giver := range givers {
		wantFR := giver == frGiver
		for _, typ := range []string{mailunsub.TypeNouvelleIdee, mailunsub.TypeRelances} {
			items := e.q.to(giver, typ)
			if len(items) == 0 {
				t.Fatalf("aucun courriel %s pour %d", typ, giver)
			}
			for _, it := range items {
				checkLang(t, it.Subject, it.Text, wantFR)
			}
		}
	}
}

// Un tireur à deux cibles reçoit un seul courriel regroupé, dans sa langue.
func TestTwoTargetsGiverGetsOneMailInItsLanguage(t *testing.T) {
	e := newEnv(t, "fr")
	gid := e.group(t, "famille")
	en := e.add(t, gid, "Evariste", "en", participants.RoleOffrePlus)
	var all []int64
	all = append(all, e.addN(t, gid, "Bertille", 2, participants.RoleReceptionSeule)...)
	all = append(all, en, e.add(t, gid, "Fulgence", "", participants.RoleOffrePlus))
	all = append(all, e.addN(t, gid, "Mederic", 2, participants.RoleNormal)...)
	e.write(t, gid, all...)
	e.mustDraw(t, gid)
	e.clock.Set(e.revealA)
	ps := e.giversPairs(t, gid)[en]
	if len(ps) != 2 {
		t.Fatalf("tireur à %d cible(s), 2 attendues", len(ps))
	}
	e.edit(t, gid, ps[0].Receiver, 1)
	e.edit(t, gid, ps[1].Receiver, 1)
	e.clock.Advance(10 * time.Minute)
	if e.sendMail(t) != 1 || e.tr.count() != 1 {
		t.Fatalf("%d courriels", e.tr.count())
	}
	items := e.q.to(en, mailunsub.TypeNouvelleIdee)
	checkLang(t, items[len(items)-1].Subject, items[len(items)-1].Text, false)
}
