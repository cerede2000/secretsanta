package adjust

import (
	"testing"

	"secretsanta/internal/participants"
)

// F4c.AC10 : le retrait d'une réception seule libère un cadeau de son
// tireur, sans aucune autre paire changée ; chaque personne restante reçoit
// toujours exactement un cadeau.
func TestRemoveReceiverFreesGiverCapacity(t *testing.T) {
	e := newEnv(t)
	gid := e.group(t, "famille", 2)
	ids := e.people(t, gid, offrePlus, normal, normal, reception, reception)
	o, a, b, r1, r2 := ids[0], ids[1], ids[2], ids[3], ids[4]
	before := []Pair{{o, a}, {o, r1}, {a, b}, {b, o}, {a, r2}}
	e.setPairs(t, gid, before)
	e.discover(t, gid, a, b)

	p := e.preview(t, gid, remove(r1))
	if p.ChangedPairs != 0 || p.Affected != 1 || p.AlreadyDiscovered != 0 {
		t.Fatalf("aperçu inattendu : %+v", p)
	}
	r := e.apply(t, gid, remove(r1))
	got := e.pairs(t, gid)
	samePairSet(t, got, []Pair{{o, a}, {a, b}, {b, o}, {a, r2}})
	checkValid(t, got, map[int64]participants.DrawRole{o: offrePlus, a: normal, b: normal, r2: reception}, 2)
	// Le tireur de la personne retirée est prévenu qu'il a une cible en moins.
	if r.Notified != 1 || len(e.mail.Sent) != 1 || e.mail.Sent[0].To != e.emails[o] {
		t.Fatalf("courriel attendu pour le seul tireur touché : %+v", e.mail.Sent)
	}
	e.onlyRecipient(t, o, e.mail.Sent[0].Subject, e.mail.Sent[0].Body)
	if e.archiver.calls[0].Giver != o || e.archiver.calls[0].OldReceiver != r1 {
		t.Fatalf("archivage inattendu : %+v", e.archiver.calls)
	}
}

// F4c.AC10 : un tireur qui n'offrait qu'à la personne retirée doit garder
// une cible ; un cadeau lui est transmis par un tireur qui en offrait deux,
// de préférence un normal (l'offre_plus garde son cadeau supplémentaire).
func TestRemoveReceiverKeepsEveryGiverGiving(t *testing.T) {
	e := newEnv(t)
	gid := e.group(t, "famille", 2)
	ids := e.people(t, gid, normal, normal, normal, reception, reception)
	a, b, c, r1, r2 := ids[0], ids[1], ids[2], ids[3], ids[4]
	// c n'offre qu'à r1 ; a offre à b et r2 ; b offre à a et c.
	before := []Pair{{a, b}, {a, r2}, {b, a}, {b, c}, {c, r1}}
	e.setPairs(t, gid, before)

	e.apply(t, gid, remove(r1))
	got := e.pairs(t, gid)
	checkValid(t, got, map[int64]participants.DrawRole{a: normal, b: normal, c: normal, r2: reception}, 2)
	if n := changedCount(before, got); n != 1 {
		t.Fatalf("%d paires changées, attendu 1 : %v", n, got)
	}
}
