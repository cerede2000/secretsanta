package letter

import (
	"reflect"
	"strings"
	"testing"
)

func TestNoGiverIdentity(t *testing.T) {
	e := newEnv(t)
	tire := e.person(t, "Alice", "normal")
	giver := e.person(t, "Zorglub", "normal")
	// Un tirage existe : le tireur de Alice est Zorglub (table propre au test).
	if _, err := e.db.Exec(`CREATE TABLE test_draw (giver INTEGER, receiver INTEGER)`); err != nil {
		t.Fatal(err)
	}
	_, _ = e.db.Exec(`INSERT INTO test_draw VALUES (?, ?)`, giver, tire)
	ctx := t.Context()
	_, _ = e.svc.SetIntro(ctx, e.group, tire, "Bonjour")
	_, _ = e.svc.AddIdea(ctx, e.group, tire, IdeaInput{Title: "Livre"})
	for _, ev := range e.events() {
		for k, v := range ev.Payload {
			for _, forbidden := range []string{"Zorglub", "zorglub"} {
				if strings.Contains(v, forbidden) {
					t.Fatalf("%s contient le tireur", k)
				}
			}
			if strings.Contains(k, "giver") || strings.Contains(k, "tireur") {
				t.Fatalf("clé %s", k)
			}
		}
		n, err := e.svc.NotificationFor(ctx, ev)
		if err != nil || n.RecipientFirstName != "Alice" {
			t.Fatalf("%+v %v", n, err)
		}
	}
	// Types exposés : aucun champ ne désigne le tireur.
	for _, typ := range []reflect.Type{reflect.TypeOf(Letter{}), reflect.TypeOf(Idea{}), reflect.TypeOf(IdeaInput{}), reflect.TypeOf(Notification{})} {
		for i := 0; i < typ.NumField(); i++ {
			n := strings.ToLower(typ.Field(i).Name)
			for _, bad := range []string{"giver", "tireur", "drawer", "santa"} {
				if strings.Contains(n, bad) {
					t.Fatalf("%s.%s", typ.Name(), typ.Field(i).Name)
				}
			}
		}
	}
	// Seul le prénom du tiré figure dans la notification.
	if reflect.TypeOf(Notification{}).NumField() != 3 {
		t.Fatal("le modèle de notification ne doit pas grossir sans revue")
	}
	// Le schéma des lettres ne contient aucune colonne de tireur.
	for _, tbl := range []string{"letters", "letter_ideas", "letter_versions", "letter_reads", "letter_archives"} {
		rows, err := e.db.Query(`SELECT name FROM pragma_table_info(?)`, tbl)
		if err != nil {
			t.Fatal(err)
		}
		for rows.Next() {
			var c string
			_ = rows.Scan(&c)
			if strings.Contains(c, "giver") || strings.Contains(c, "drawer") {
				t.Fatalf("%s.%s", tbl, c)
			}
		}
		rows.Close()
	}
}
