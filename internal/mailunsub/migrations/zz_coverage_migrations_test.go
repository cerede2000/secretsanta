package migrations

import "testing"

func TestMigrationsEmbarquees(t *testing.T) {
	ms, err := Migrations()
	if err != nil {
		t.Fatalf("Migrations() : %v", err)
	}
	if len(ms) == 0 {
		t.Fatal("aucune migration embarquée")
	}
	prev := 0
	for _, m := range ms {
		if m.Version <= 0 || m.Name == "" || m.SQL == "" {
			t.Errorf("migration incomplète : %+v", m)
		}
		if m.Version <= prev {
			t.Errorf("versions non strictement croissantes : %d après %d", m.Version, prev)
		}
		prev = m.Version
	}
}
