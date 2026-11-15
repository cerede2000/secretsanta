package letterread

import (
	"encoding/json"
	"sort"
	"testing"

	"secretsanta/internal/draw"
	"secretsanta/internal/letter"
	"secretsanta/internal/participants"
)

// AC4 : l'archivage lors d'un ajustement retire la lettre à l'ancien tireur
// et ne la montre pas à un nouveau (l'archive n'est jamais servie ; le
// nouveau tireur reçoit la lettre existante, sans le cachet de l'ancien).
func TestArchiveOnAdjust(t *testing.T) {
	e := newEnv(t)
	gid := e.group(t, "ajustement")
	ids := e.addN(t, gid, "Amandine", 5, participants.RoleNormal)
	e.write(t, gid, ids...)
	e.mustDraw(t, gid)
	e.openReveal()
	e.discoverAll(t, gid)

	// Tout le monde lit sa cible : chaque lettre porte le cachet.
	for _, p := range e.pairs(t, gid) {
		e.mustRead(t, gid, p.Giver, p.ID, p.Receiver)
	}
	tire := ids[0]
	old := e.pairOf(t, gid, tire)
	if !e.stamp(t, gid, tire) {
		t.Fatal("cachet attendu avant l'ajustement")
	}
	l0, _ := e.letters.Get(ctx, gid, tire)

	after, newGiver, otherTire := e.swap(t, gid, tire)
	changed := e.adjust(t, gid, after)
	sort.Slice(changed, func(i, j int) bool { return changed[i] < changed[j] })
	want := []int64{tire, otherTire}
	sort.Slice(want, func(i, j int) bool { return want[i] < want[j] })
	if len(changed) != 2 || changed[0] != want[0] || changed[1] != want[1] {
		t.Fatalf("tirés archivés %v, attendus %v", changed, want)
	}

	// L'ancien tireur ne lit plus la lettre de son ancienne cible.
	_, err := e.svc.Read(ctx, who(gid, old.Giver), gid, old.ID)
	wantRefused(t, err)
	mine, _ := e.draw.MyTargets(ctx, who(gid, old.Giver), gid)
	for _, tg := range mine.Targets {
		if tg.ReceiverID == tire {
			t.Fatal("l'ancien tireur garde la cible")
		}
		// Sa nouvelle cible est à découvrir avant d'être lue.
		if tg.ReceiverID == otherTire {
			_, err := e.svc.Read(ctx, who(gid, old.Giver), gid, tg.PairID)
			wantRefused(t, err)
		}
	}

	// La lettre est conservée : un instantané de la version lue.
	if e.archives(t, tire) != 1 || e.archives(t, otherTire) != 1 {
		t.Fatalf("archives : %d / %d", e.archives(t, tire), e.archives(t, otherTire))
	}
	var ver int
	var raw string
	if err := e.db.QueryRow(`SELECT a.version, a.snapshot FROM letter_archives a JOIN letters l ON l.id = a.letter_id
		WHERE l.participant_id = ?`, tire).Scan(&ver, &raw); err != nil {
		t.Fatal(err)
	}
	var snap struct {
		Version int
		Ideas   []struct{ Title string }
	}
	if err := json.Unmarshal([]byte(raw), &snap); err != nil || ver != l0.Version || snap.Version != l0.Version ||
		len(snap.Ideas) != 1 || snap.Ideas[0].Title != ideaTitle(tire) {
		t.Fatalf("instantané : %d %s %v", ver, raw, err)
	}
	// Les lettres des paires inchangées ne sont pas archivées et gardent
	// leur cachet.
	for _, o := range ids {
		if o == tire || o == otherTire {
			continue
		}
		if e.archives(t, o) != 0 || !e.stamp(t, gid, o) {
			t.Fatalf("lettre de %d touchée par l'ajustement", o)
		}
	}

	// Côté tiré : le cachet de l'ancien tireur ne vaut plus.
	if e.stamp(t, gid, tire) {
		t.Fatal("le cachet de l'ancien tireur est resté")
	}
	if adm, _ := e.svc.AdminRead(ctx, admin(gid), gid); adm[tire] {
		t.Fatal("« lettre lue » de l'ancien tireur resté")
	}

	// Le nouveau tireur : rien avant d'avoir découvert sa nouvelle cible.
	np := e.pairOf(t, gid, tire)
	// L'identifiant de paire peut être réutilisé par SQLite : seul le tireur compte.
	if np.Giver != newGiver {
		t.Fatalf("nouvelle paire inattendue : %+v", np)
	}
	_, err = e.svc.Read(ctx, who(gid, newGiver), gid, np.ID)
	wantRefused(t, err)
	_, err = e.svc.Read(ctx, who(gid, newGiver), gid, old.ID)
	wantRefused(t, err)
	e.discover(t, gid, newGiver, np.ID)

	// Le tiré continue d'écrire après l'ajustement : le nouveau tireur lit la
	// lettre actuelle, jamais l'instantané archivé.
	if _, err := e.letters.AddIdea(ctx, gid, tire, letter.IdeaInput{Title: "Après l'ajustement"}); err != nil {
		t.Fatal(err)
	}
	l := e.mustRead(t, gid, newGiver, np.ID, tire)
	if l.Version != l0.Version+1 || len(l.Ideas) != 2 || l.Ideas[1].Title != "Après l'ajustement" {
		t.Fatalf("lettre actuelle attendue : %+v", l)
	}
	if !e.stamp(t, gid, tire) {
		t.Fatal("cachet attendu après lecture du nouveau tireur")
	}
	// L'archive reste intacte.
	var raw2 string
	_ = e.db.QueryRow(`SELECT a.snapshot FROM letter_archives a JOIN letters l ON l.id = a.letter_id
		WHERE l.participant_id = ?`, tire).Scan(&raw2)
	if raw2 != raw {
		t.Fatal("l'archive a changé")
	}

	// Un ajustement qui ne change pas le tireur d'un tiré n'archive rien.
	var same []draw.Pair
	for _, p := range e.pairs(t, gid) {
		same = append(same, draw.Pair{Giver: p.Giver, Receiver: p.Receiver})
	}
	if c := e.adjust(t, gid, same); len(c) != 0 {
		t.Fatalf("archivage sans changement : %v", c)
	}
	if e.archives(t, tire) != 1 || !e.stamp(t, gid, tire) {
		t.Fatal("un ajustement sans effet a touché la lettre")
	}

	// Un tiré sans lettre écrite : l'archivage l'ignore sans erreur.
	tx, err := e.db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := e.svc.ArchiveTx(ctx, tx, gid, []int64{999999}); err != nil {
		t.Fatal(err)
	}
	_ = tx.Rollback()
}
