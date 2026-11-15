package adminnav

import "testing"

func TestOrderAndActive(t *testing.T) {
	r := New()
	// Déclarations indépendantes, dans le désordre.
	r.Declare(Entry{Key: "plus", Label: "Plus", Rank: RankMore, Destination: "/admin/plus"})
	r.Declare(Entry{Key: "inscriptions", Label: "Inscriptions", Rank: RankRegistrations, Destination: "/admin/inscriptions"})
	r.Declare(Entry{Key: "tirage", Label: "Tirage", Rank: RankDraw})
	r.Declare(Entry{Key: "tableau", Label: "Tableau", Rank: RankDashboard})
	r.Declare(Entry{Key: "participants", Label: "Participants", Rank: RankParticipants})

	items := r.Items("tirage")
	want := []string{"Tableau", "Participants", "Tirage", "Inscriptions", "Plus"}
	if len(items) != len(want) {
		t.Fatalf("%d entrées", len(items))
	}
	for i, w := range want {
		if items[i].Label != w {
			t.Fatalf("rang %d: %s, attendu %s", i, items[i].Label, w)
		}
		if items[i].Active != (w == "Tirage") {
			t.Fatalf("active incorrecte pour %s", w)
		}
	}
}

func TestNoActive(t *testing.T) {
	r := New()
	r.Declare(Entry{Key: "a", Rank: 1})
	if r.Items("zz")[0].Active {
		t.Fatal("aucune entrée active attendue")
	}
}
