package reveal

import (
	"errors"
	"fmt"
	"reflect"
	"testing"
	"time"

	"secretsanta/internal/mail/templates"
	"secretsanta/internal/participants"
)

// AC16 : les données du courriel « tirage prêt » portent le nombre de
// cibles (zéro pour reception_seule, une ou deux) sans aucun nom.
func TestReadyMailVariantsCarryOnlyTargetCounts(t *testing.T) {
	e := newEnv(t)
	at := t0.Add(24 * time.Hour)
	gid := e.group(t, "variantes", &at)
	reception := e.addN(t, gid, "Gwenaelle", 2, participants.RoleReceptionSeule)
	more := e.addN(t, gid, "Barnabe", 2, participants.RoleOffrePlus)
	normal := e.addN(t, gid, "Cunegonde", 2, participants.RoleNormal)
	o := e.mustDraw(t, gid)
	raw := e.rawPairs(t, o.DrawID)

	e.clock.Set(at)
	e.tick(t)
	sent := e.mail.notices()
	all := append(append(append([]int64{}, reception...), more...), normal...)
	if len(sent) != len(all) {
		t.Fatalf("%d avis, %d attendus (un par personne)", len(sent), len(all))
	}
	byID := map[int64]ReadyNotice{}
	for _, n := range sent {
		if _, dup := byID[n.ParticipantID]; dup {
			t.Fatalf("deux avis pour %d", n.ParticipantID)
		}
		byID[n.ParticipantID] = n
		if n.GroupID != gid || n.DrawID != o.DrawID {
			t.Fatalf("avis d'un autre tirage : %+v", n)
		}
		if want := len(receiversOfRaw(raw, n.ParticipantID)); n.Targets != want {
			t.Fatalf("participant %d : %d cible(s) annoncée(s), %d en base", n.ParticipantID, n.Targets, want)
		}
		e.noNames(t, fmt.Sprintf("%+v %#v", n, n))
	}
	for _, id := range reception {
		n := byID[id]
		if n.Targets != 0 || !n.ReceptionOnly || templates.ReadyKind(n.ReceptionOnly, n.Targets) != templates.ReadyReception {
			t.Fatalf("réception seule : %+v", n)
		}
	}
	for _, id := range more {
		n := byID[id]
		if n.Targets != 2 || n.ReceptionOnly || templates.ReadyKind(n.ReceptionOnly, n.Targets) != templates.ReadyTwo {
			t.Fatalf("« J'offre plus » : %+v", n)
		}
	}
	for _, id := range normal {
		n := byID[id]
		if n.Targets != 1 || n.ReceptionOnly || templates.ReadyKind(n.ReceptionOnly, n.Targets) != templates.ReadyOne {
			t.Fatalf("normal : %+v", n)
		}
	}

	// La donnée ne porte que des identifiants et des nombres : aucune
	// chaîne (donc aucun nom) et aucun champ désignant une autre personne.
	ty := reflect.TypeOf(ReadyNotice{})
	for i := 0; i < ty.NumField(); i++ {
		switch ty.Field(i).Type.Kind() {
		case reflect.Int, reflect.Int64, reflect.Bool:
		default:
			t.Fatalf("champ %s de type %s dans l'avis", ty.Field(i).Name, ty.Field(i).Type)
		}
	}
	noPairType(t, ReadyNotice{})
	// Aucun identifiant de destinataire : la valeur de chaque champ entier
	// est soit l'identifiant de la personne, du groupe ou du tirage, soit
	// son nombre de cibles.
	want := map[string]bool{"GroupID": true, "DrawID": true, "ParticipantID": true, "Targets": true, "ReceptionOnly": true}
	for i := 0; i < ty.NumField(); i++ {
		if !want[ty.Field(i).Name] {
			t.Fatalf("champ inattendu %s dans l'avis", ty.Field(i).Name)
		}
	}
	// La file interne ne stocke elle aussi que des nombres.
	cols := e.columns(t, "reveal_notices")
	if !reflect.DeepEqual(cols, []string{"id", "group_id", "draw_id", "participant_id", "targets", "created_at", "sent_at"}) {
		t.Fatalf("colonnes de la file : %v", cols)
	}
}

func TestReadyMailRetriedAfterQueueFailure(t *testing.T) {
	e := newEnv(t)
	gid := e.group(t, "file-en-panne", nil)
	ids := e.addN(t, gid, "Firmin", 3, participants.RoleNormal)
	e.mail.err = errors.New("file indisponible")
	o := e.mustDraw(t, gid)
	e.wantOpen(t, gid, o.DrawID, ids)
	if n := len(e.mail.notices()); n != 0 {
		t.Fatalf("%d avis malgré la panne", n)
	}
	if n := e.count(t, `SELECT COUNT(*) FROM reveal_notices WHERE sent_at IS NULL`); n != len(ids) {
		t.Fatalf("%d avis en attente, %d attendus", n, len(ids))
	}
	e.mail.err = nil
	e.clock.Advance(DefaultRetryAfter)
	e.tick(t)
	if n := len(e.mail.notices()); n != len(ids) {
		t.Fatalf("%d avis après reprise, %d attendus", n, len(ids))
	}
	e.clock.Advance(DefaultRetryAfter)
	e.tick(t)
	if err := e.svc.Flush(ctx); err != nil {
		t.Fatal(err)
	}
	if n := len(e.mail.notices()); n != len(ids) {
		t.Fatalf("avis en double : %d", n)
	}
}

func (e *env) columns(t *testing.T, table string) []string {
	t.Helper()
	rows, err := e.db.Query(`SELECT name FROM pragma_table_info(?) ORDER BY cid`, table)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			t.Fatal(err)
		}
		out = append(out, n)
	}
	return out
}
