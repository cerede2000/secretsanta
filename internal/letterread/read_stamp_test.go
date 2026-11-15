package letterread

import (
	"reflect"
	"sort"
	"strings"
	"testing"

	"secretsanta/internal/letter"
	"secretsanta/internal/participants"
)

// AC3 : le cachet « lue par le Père Noël » apparaît côté tiré après la
// lecture du tireur, sans aucune information d'identité, et disparaît quand
// la lettre est modifiée.
func TestReadStamp(t *testing.T) {
	e := newEnv(t)
	gid := e.group(t, "cachet")
	ids := e.addN(t, gid, "Amandine", 4, participants.RoleNormal)
	// ids[3] n'écrit pas encore de lettre.
	e.write(t, gid, ids[:3]...)
	e.mustDraw(t, gid)
	e.openReveal()

	// Le cachet ne porte qu'un booléen : ni date, ni identité.
	st := reflect.TypeOf(Stamp{})
	if st.NumField() != 1 || st.Field(0).Name != "Read" || st.Field(0).Type.Kind() != reflect.Bool {
		t.Fatalf("le cachet ne doit contenir que Read bool : %v", st)
	}
	// Et la table du cachet ne garde ni date ni tireur.
	rows, err := e.db.Query(`SELECT name FROM pragma_table_info('letterread_stamps')`)
	if err != nil {
		t.Fatal(err)
	}
	var cols []string
	for rows.Next() {
		var c string
		_ = rows.Scan(&c)
		cols = append(cols, c)
	}
	rows.Close()
	sort.Strings(cols)
	if strings.Join(cols, ",") != "letter_id,pair_id,version" {
		t.Fatalf("colonnes du cachet : %v", cols)
	}

	tire := ids[0]
	p := e.pairOf(t, gid, tire)
	if e.stamp(t, gid, tire) {
		t.Fatal("cachet avant toute lecture")
	}
	// La découverte de la cible ne pose pas le cachet : seule la lecture.
	e.discoverAll(t, gid)
	if e.stamp(t, gid, tire) {
		t.Fatal("cachet posé par la seule découverte")
	}
	// Une tentative de lecture par un autre participant ne le pose pas.
	for _, o := range ids {
		if o != p.Giver {
			_, _ = e.svc.Read(ctx, who(gid, o), gid, p.ID)
		}
	}
	if e.stamp(t, gid, tire) {
		t.Fatal("cachet posé par quelqu'un d'autre que le tireur")
	}

	e.mustRead(t, gid, p.Giver, p.ID, tire)
	got, err := e.svc.Stamp(ctx, who(gid, tire), gid)
	if err != nil || got != (Stamp{Read: true}) {
		t.Fatalf("cachet attendu après lecture : %+v %v", got, err)
	}
	// Le cachet ne concerne que cette lettre.
	for _, o := range ids[1:] {
		if e.stamp(t, gid, o) {
			t.Fatalf("cachet posé sur la lettre de %d", o)
		}
	}
	// L'administrateur voit seulement « lettre lue », par tiré.
	adm, err := e.svc.AdminRead(ctx, admin(gid), gid)
	if err != nil || len(adm) != 1 || !adm[tire] {
		t.Fatalf("vue administrateur : %v %v", adm, err)
	}
	if _, err := e.svc.AdminRead(ctx, who(gid, tire), gid); err == nil {
		t.Fatal("un participant a obtenu la vue administrateur")
	}
	if _, err := e.svc.Stamp(ctx, admin(gid), gid); err == nil {
		t.Fatal("le cachet n'est servi qu'au tiré")
	}

	// Relire la même version ne change rien.
	e.mustRead(t, gid, p.Giver, p.ID, tire)
	if !e.stamp(t, gid, tire) {
		t.Fatal("cachet perdu après relecture")
	}

	// Une modification de la lettre retire le cachet...
	if _, err := e.letters.SetIntro(ctx, gid, tire, "Cher Père Noël"); err != nil {
		t.Fatal(err)
	}
	if e.stamp(t, gid, tire) {
		t.Fatal("cachet resté après modification")
	}
	if adm, _ := e.svc.AdminRead(ctx, admin(gid), gid); adm[tire] {
		t.Fatal("« lettre lue » resté après modification")
	}
	// ... jusqu'à la prochaine lecture.
	l := e.mustRead(t, gid, p.Giver, p.ID, tire)
	if l.Intro != "Cher Père Noël" {
		t.Fatalf("version actuelle attendue : %+v", l)
	}
	if !e.stamp(t, gid, tire) {
		t.Fatal("cachet absent après lecture de la nouvelle version")
	}
	// Une idée ajoutée retire aussi le cachet.
	if _, err := e.letters.AddIdea(ctx, gid, tire, letter.IdeaInput{Title: "Encore"}); err != nil {
		t.Fatal(err)
	}
	if e.stamp(t, gid, tire) {
		t.Fatal("cachet resté après ajout d'une idée")
	}

	// Lettre jamais écrite : la lecture ne pose aucun cachet ; la première
	// version reste à lire.
	late := ids[3]
	lp := e.pairOf(t, gid, late)
	l, err = e.svc.Read(ctx, who(gid, lp.Giver), gid, lp.ID)
	if err != nil || l.Version != 0 || len(l.Ideas) != 0 {
		t.Fatalf("lettre vide attendue : %+v %v", l, err)
	}
	if e.stamp(t, gid, late) {
		t.Fatal("cachet sur une lettre jamais écrite")
	}
	e.write(t, gid, late)
	if e.stamp(t, gid, late) {
		t.Fatal("cachet sur une version jamais lue")
	}
	e.mustRead(t, gid, lp.Giver, lp.ID, late)
	if !e.stamp(t, gid, late) {
		t.Fatal("cachet absent après lecture")
	}

	// Un nouveau tirage complet : le nouveau tireur n'a rien lu.
	if _, err := e.draw.Redraw(ctx, admin(gid), gid, drawReq()); err != nil {
		t.Fatal(err)
	}
	for _, o := range ids {
		if e.stamp(t, gid, o) {
			t.Fatalf("cachet de l'ancien tirage resté pour %d", o)
		}
	}
}
