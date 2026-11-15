package givernotify

import (
	"testing"

	"secretsanta/internal/participants"
)

// F7b.AC5 : une personne reception_seule ne reçoit jamais la notification de
// nouvelle idée ni la relance du tireur.
func TestReceptionOnlyNeverNotified(t *testing.T) {
	e := newEnv(t, "fr")
	gid, all := e.twoTargetsGroup(t, "famille")
	var reception []int64
	for _, id := range all {
		var role string
		if err := e.db.QueryRow(`SELECT draw_role FROM participants WHERE id = ?`, id).Scan(&role); err != nil {
			t.Fatal(err)
		}
		if participants.DrawRole(role) == participants.RoleReceptionSeule {
			reception = append(reception, id)
		}
	}
	if len(reception) != 2 {
		t.Fatalf("%d personnes reception_seule", len(reception))
	}
	for i, id := range all {
		e.edit(t, gid, id, i)
	}
	e.clock.Advance(3 * day)
	e.remind(t)
	e.clock.Advance(30 * day)
	e.remind(t)
	e.sendMail(t)
	for _, id := range reception {
		if got := e.q.to(id, ""); len(got) != 0 {
			t.Fatalf("la personne %d (reception_seule) a reçu %d courriel(s)", id, len(got))
		}
	}
	if len(e.q.items) == 0 {
		t.Fatal("les autres tireurs auraient dû être notifiés")
	}
}

// Même une paire incohérente (tireur reception_seule) ne produit rien.
func TestReceptionOnlyGiverPairIgnored(t *testing.T) {
	e := newEnv(t, "fr")
	gid, all := e.twoTargetsGroup(t, "famille")
	rec := all[0] // reception_seule
	var target rawPair
	for _, p := range e.pairs(t, gid) {
		if p.Receiver != rec {
			target = p
			break
		}
	}
	if _, err := e.db.Exec(`DELETE FROM draw_pairs WHERE id = ?`, target.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := e.db.Exec(`INSERT INTO draw_pairs (draw_id, giver_id, receiver_id)
		SELECT draw_id, ?, ? FROM draw_pairs LIMIT 1`, rec, target.Receiver); err != nil {
		t.Fatal(err)
	}
	if _, err := e.db.Exec(`UPDATE participants SET draw_role = 'reception_seule' WHERE id = ?`, rec); err != nil {
		t.Fatal(err)
	}
	e.edit(t, gid, target.Receiver, 1)
	e.clock.Advance(4 * day)
	e.remind(t)
	if got := e.q.to(rec, ""); len(got) != 0 {
		t.Fatalf("%d courriel(s) pour une personne reception_seule", len(got))
	}
}
