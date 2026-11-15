package adminconsult

import (
	"net/http"
	"net/url"
	"reflect"
	"strings"
	"testing"
	"time"

	"secretsanta/internal/participants"
	"secretsanta/internal/shared/ports"
)

// F10b.AC3 : chaque consultation ajoute une ligne au journal, que TOUS les
// participants du groupe lisent ; la ligne ne porte qu'une date.
func TestEveryConsultationIsInTheJournalOfAllParticipants(t *testing.T) {
	e := newEnv(t)
	gid := e.group("journal", true)
	ids := e.addN(gid, "Hortense", 5)
	e.doDraw(gid)
	admin := participants.AdminActor(gid)
	if _, err := e.svc.Consult(ctx, admin, gid, ids[0], e.secrets[gid]); err != nil {
		t.Fatal(err)
	}
	e.clock.Advance(26 * time.Hour)
	if _, err := e.svc.Consult(ctx, admin, gid, ids[3], e.secrets[gid]); err != nil {
		t.Fatal(err)
	}
	want := []time.Time{t0.Add(26 * time.Hour), t0}
	for _, pid := range ids {
		got, err := e.svc.Journal(ctx, participants.ParticipantActor(gid, pid), gid)
		if err != nil {
			t.Fatalf("participant %d : %v", pid, err)
		}
		if len(got) != len(want) {
			t.Fatalf("participant %d : %d ligne(s), attendu %d", pid, len(got), len(want))
		}
		for i := range want {
			if !got[i].At.Equal(want[i]) {
				t.Errorf("participant %d, ligne %d : %v, attendu %v", pid, i, got[i].At, want[i])
			}
		}
	}
	if got, err := e.svc.Journal(ctx, admin, gid); err != nil || len(got) != 2 {
		t.Fatalf("administrateur : %d, %v", len(got), err)
	}
	// Ni un visiteur ni un autre groupe ne lisent ce journal.
	other := e.group("autre", true)
	otherIDs := e.addN(other, "Ines", 3)
	if _, err := e.svc.Journal(ctx, participants.Actor{}, gid); err == nil {
		t.Error("un visiteur lit le journal")
	}
	if _, err := e.svc.Journal(ctx, participants.ParticipantActor(other, otherIDs[0]), gid); err == nil {
		t.Error("un participant d'un autre groupe lit le journal")
	}
	if got, err := e.svc.Journal(ctx, participants.ParticipantActor(other, otherIDs[0]), other); err != nil || len(got) != 0 {
		t.Errorf("l'autre groupe hérite des consultations : %d, %v", len(got), err)
	}
}

// F10b.AC3 : la page du journal, vue par chaque participant, dit
// « l'organisateur a consulté une paire » avec la date, sans aucun nom.
func TestJournalPageShowsNoName(t *testing.T) {
	e := newEnv(t)
	gid := e.group("page-journal", true)
	ids := e.addN(gid, "Hortense", 4)
	e.doDraw(gid)
	if rec := e.consult("page-journal", "fr", e.adminCookie(gid), url.Values{"person": {idStr(ids[1])}, "password": {e.secrets[gid]}}); rec.Code != http.StatusOK {
		t.Fatalf("consultation : %d", rec.Code)
	}
	for _, pid := range ids {
		rec := e.get("/page-journal/consultations", "fr", e.participantCookie(gid, pid))
		if rec.Code != http.StatusOK {
			t.Fatalf("participant %d : %d", pid, rec.Code)
		}
		body := strings.ReplaceAll(rec.Body.String(), "&#39;", "'")
		if !strings.Contains(body, "L'organisateur a consulté une paire.") || !strings.Contains(body, "2026-12-12") {
			t.Fatalf("participant %d : ligne du journal absente", pid)
		}
		for _, id := range ids {
			for _, n := range e.names[id] {
				if strings.Contains(body, n) {
					t.Errorf("participant %d : le nom %q apparaît dans le journal", pid, n)
				}
			}
		}
	}
	if rec := e.get("/page-journal/consultations", "fr", e.adminCookie(gid)); rec.Code != http.StatusOK {
		t.Fatalf("administrateur : %d", rec.Code)
	}
	if rec := e.get("/page-journal/consultations", "fr", nil); rec.Code != http.StatusSeeOther {
		t.Fatalf("visiteur : %d", rec.Code)
	}
}

// F10b.AC3 : le journal ne peut porter aucun nom (ni colonne, ni champ) et
// reste en ajout seul.
func TestJournalStoresNoName(t *testing.T) {
	e := newEnv(t)
	rows, err := e.db.Query(`SELECT name FROM pragma_table_info('adminconsult_journal') ORDER BY cid`)
	if err != nil {
		t.Fatal(err)
	}
	var cols []string
	for rows.Next() {
		var c string
		if err := rows.Scan(&c); err != nil {
			t.Fatal(err)
		}
		cols = append(cols, c)
	}
	_ = rows.Close()
	if strings.Join(cols, ",") != "id,group_id,at" {
		t.Fatalf("colonnes du journal : %v (aucune personne ne doit y figurer)", cols)
	}
	et := reflect.TypeOf(Entry{})
	if et.NumField() != 1 || et.Field(0).Name != "At" {
		t.Fatalf("Entry ne doit porter qu'une date : %v", et)
	}
	gid := e.group("ajout-seul", true)
	ids := e.addN(gid, "Hortense", 3)
	e.doDraw(gid)
	if _, err := e.svc.Consult(ctx, participants.AdminActor(gid), gid, ids[0], e.secrets[gid]); err != nil {
		t.Fatal(err)
	}
	if _, err := e.db.Exec(`UPDATE adminconsult_journal SET at = 0 WHERE group_id = ?`, gid); err == nil {
		t.Fatal("une ligne du journal a pu être réécrite")
	}
}

// F10b.AC3 : aucune consultation n'envoie de courriel. Le service ne dépend
// d'aucun moyen d'envoi : ni dans son constructeur, ni dans ses champs.
func TestConsultationSendsNoEmail(t *testing.T) {
	sender := reflect.TypeOf((*ports.EmailSender)(nil)).Elem()
	mailish := func(t reflect.Type) bool {
		for t.Kind() == reflect.Pointer {
			t = t.Elem()
		}
		p := t.PkgPath()
		return strings.Contains(p, "/mail") || strings.Contains(p, "mailmsg") || strings.Contains(strings.ToLower(t.Name()), "mail")
	}
	ctor := reflect.TypeOf(NewService)
	for i := 0; i < ctor.NumIn(); i++ {
		in := ctor.In(i)
		if mailish(in) || (in.Kind() == reflect.Interface && in.Implements(sender)) || reflect.PointerTo(in).Implements(sender) {
			t.Errorf("NewService reçoit un moyen d'envoi : %v", in)
		}
	}
	st := reflect.TypeOf(Service{})
	for i := 0; i < st.NumField(); i++ {
		f := st.Field(i).Type
		if mailish(f) || f.Implements(sender) {
			t.Errorf("Service.%s peut envoyer un courriel : %v", st.Field(i).Name, f)
		}
	}
	// Et la consultation passe sans aucune file d'envoi en base.
	e := newEnv(t)
	gid := e.group("silence", true)
	ids := e.addN(gid, "Hortense", 3)
	e.doDraw(gid)
	if _, err := e.svc.Consult(ctx, participants.AdminActor(gid), gid, ids[0], e.secrets[gid]); err != nil {
		t.Fatal(err)
	}
	if n := countRows(t, e.db, `SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name LIKE '%mail%'`); n != 0 {
		t.Fatalf("fixture : %d table(s) de courriel inattendue(s)", n)
	}
}
