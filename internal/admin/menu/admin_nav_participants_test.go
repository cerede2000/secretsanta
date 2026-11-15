package menu

import (
	"testing"

	"secretsanta/internal/shared/adminnav"
)

// F10.AC11 : « Participants » se déclare seule, sans les autres entrées de F10.
func TestParticipantsDeclaredAlone(t *testing.T) {
	r := adminnav.New()
	DeclareParticipants(r)
	items := r.Items("participants")
	if len(items) != 1 {
		t.Fatalf("%d entrées, 1 attendue", len(items))
	}
	it := items[0]
	if it.Key != "participants" || it.Label != "layout.nav.participants" || it.Rank != adminnav.RankParticipants || !it.Active {
		t.Errorf("entrée inattendue : %+v", it)
	}
	if it.Destination != ParticipantsPath {
		t.Errorf("destination %q", it.Destination)
	}
}

// Elle se place entre Tableau et Tirage, quel que soit l'ordre de déclaration.
func TestParticipantsRank(t *testing.T) {
	r := adminnav.New()
	DeclareNav(r)
	DeclareParticipants(r)
	var keys []string
	for _, it := range r.Items("") {
		keys = append(keys, it.Key)
	}
	want := []string{"tableau", "participants", "tirage", "plus"}
	if len(keys) != len(want) {
		t.Fatalf("%v", keys)
	}
	for i := range want {
		if keys[i] != want[i] {
			t.Fatalf("ordre %v, attendu %v", keys, want)
		}
	}
}
