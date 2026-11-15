package adjust

import (
	"errors"
	"strings"
	"testing"

	"secretsanta/internal/draw"
)

// mailMentions vérifie qu'un courriel ne nomme que son destinataire.
func (e *env) onlyRecipient(t *testing.T, to int64, subject, body string) {
	t.Helper()
	text := strings.ToLower(subject + "\n" + body)
	if !strings.Contains(text, strings.ToLower(e.firsts[to])) {
		t.Fatalf("le courriel ne salue pas son destinataire : %q", body)
	}
	for id, first := range e.firsts {
		if id == to {
			continue
		}
		if strings.Contains(text, strings.ToLower(first)) {
			t.Fatalf("le courriel à %d nomme une autre personne (%q) : %q", to, first, body)
		}
		if strings.Contains(text, e.emails[id]) {
			t.Fatalf("le courriel à %d cite une autre adresse : %q", to, body)
		}
	}
	if strings.Contains(text, "zx") {
		t.Fatalf("le courriel cite un nom de famille : %q", body)
	}
}

func (e *env) events(t *testing.T, gid int64, kind string) int {
	t.Helper()
	return e.count(t, `SELECT COUNT(*) FROM draw_events WHERE group_id = ? AND kind = ?`, gid, kind)
}

// F4c.AC4 : retrait — la personne dont la cible change reçoit un courriel qui
// ne nomme qu'elle, la lettre de son ancienne cible est archivée dans la
// transaction et l'ajustement est consigné dans l'historique.
func TestRemoveEffects(t *testing.T) {
	e := newEnv(t)
	gid := e.group(t, "famille", 2)
	ids := e.people(t, gid, normal, normal, normal, normal)
	a, b, c, x := ids[0], ids[1], ids[2], ids[3]
	e.setPairs(t, gid, ring(a, b, c, x))
	adjBefore := e.events(t, gid, draw.KindAdjustment)

	r := e.apply(t, gid, remove(x))

	if len(e.mail.Sent) != 1 || r.Notified != 1 || r.NotifyFailed != 0 {
		t.Fatalf("un seul courriel attendu : %+v / %+v", e.mail.Sent, r)
	}
	m := e.mail.Sent[0]
	if m.To != e.emails[c] {
		t.Fatalf("courriel envoyé à %q, attendu la personne dont la cible change", m.To)
	}
	if m.Type != EmailTypeDrawAdjusted || !m.Essential {
		t.Fatalf("nature de courriel inattendue : %+v", m)
	}
	e.onlyRecipient(t, c, m.Subject, m.Body)

	want := []archiveCall{{GroupID: gid, Giver: c, OldReceiver: x, InTx: true}}
	if len(e.archiver.calls) != 1 || e.archiver.calls[0] != want[0] {
		t.Fatalf("archivage %+v, attendu %+v", e.archiver.calls, want)
	}
	if got := e.events(t, gid, draw.KindAdjustment); got != adjBefore+1 {
		t.Fatalf("historique : %d ajustements, attendu %d", got, adjBefore+1)
	}
	h, err := e.draw.History(ctx, admin(gid), gid)
	if err != nil {
		t.Fatal(err)
	}
	last := h[0].Events[len(h[0].Events)-1]
	if last.Kind != draw.KindAdjustment || last.Revision != r.Revision {
		t.Fatalf("dernier événement %+v, révision %d", last, r.Revision)
	}
}

// F4c.AC4 : ajout — la personne dont la paire est coupée et la personne
// ajoutée sont prévenues, chacune sans autre nom que le sien.
func TestAddEffects(t *testing.T) {
	e := newEnv(t)
	gid := e.group(t, "famille", 2)
	ids := e.people(t, gid, normal, normal, normal)
	a, b, c := ids[0], ids[1], ids[2]
	e.setPairs(t, gid, ring(a, b, c))
	e.discover(t, gid, a, b)
	e.discover(t, gid, b, c)
	w := e.add(t, gid, "Nouveau", normal)

	r := e.apply(t, gid, addP(w))
	if r.Notified != 2 || len(e.mail.Sent) != 2 {
		t.Fatalf("deux courriels attendus : %+v", e.mail.Sent)
	}
	byMail := map[string]int64{e.emails[c]: c, e.emails[w]: w}
	for _, m := range e.mail.Sent {
		to, ok := byMail[m.To]
		if !ok {
			t.Fatalf("courriel inattendu à %q", m.To)
		}
		e.onlyRecipient(t, to, m.Subject, m.Body)
		delete(byMail, m.To)
	}
	// c perd a (archivée) ; w n'avait pas de cible.
	if len(e.archiver.calls) != 1 || e.archiver.calls[0].Giver != c || e.archiver.calls[0].OldReceiver != a {
		t.Fatalf("archivage inattendu : %+v", e.archiver.calls)
	}
}

// Un échec de l'archivage annule tout l'ajustement : aucune paire, aucun
// retrait, aucun historique, aucun courriel.
func TestArchiveFailureRollsBack(t *testing.T) {
	e := newEnv(t)
	gid := e.group(t, "famille", 2)
	ids := e.people(t, gid, normal, normal, normal, normal)
	e.setPairs(t, gid, ring(ids...))
	before := e.rows(t, gid)
	adjBefore := e.events(t, gid, draw.KindAdjustment)
	e.archiver.err = errors.New("archive indisponible")

	p := e.preview(t, gid, remove(ids[3]))
	_, err := e.svc.Apply(ctx, admin(gid), gid, remove(ids[3]), Confirmation{Confirmed: true, Revision: p.Revision})
	if err == nil {
		t.Fatal("échec attendu")
	}
	if got := e.rows(t, gid); len(got) != len(before) {
		t.Fatalf("paires modifiées malgré l'échec : %v", got)
	}
	for i, r := range e.rows(t, gid) {
		if r != before[i] {
			t.Fatalf("paire modifiée malgré l'échec : %+v", r)
		}
	}
	if e.count(t, `SELECT COUNT(*) FROM participants WHERE id = ?`, ids[3]) != 1 {
		t.Fatal("la personne a été retirée malgré l'échec")
	}
	if e.events(t, gid, draw.KindAdjustment) != adjBefore || len(e.mail.Sent) != 0 {
		t.Fatal("historique ou courriel malgré l'échec")
	}
}

// Un courriel qui ne part pas ne défait pas l'ajustement : il est compté.
func TestMailFailureIsCounted(t *testing.T) {
	e := newEnv(t)
	gid := e.group(t, "famille", 2)
	ids := e.people(t, gid, normal, normal, normal, normal)
	e.setPairs(t, gid, ring(ids...))
	e.mail.Err = errors.New("serveur de courriel indisponible")
	r := e.apply(t, gid, remove(ids[3]))
	if r.Notified != 0 || r.NotifyFailed != 1 {
		t.Fatalf("échec de courriel non compté : %+v", r)
	}
	if e.count(t, `SELECT COUNT(*) FROM participants WHERE id = ?`, ids[3]) != 0 {
		t.Fatal("l'ajustement doit être fait")
	}
}
