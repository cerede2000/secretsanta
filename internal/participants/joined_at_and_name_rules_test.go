package participants

import (
	"testing"
	"time"
)

func TestJoinedAtIsCreationInstant(t *testing.T) {
	e := newEnv(t)
	e.clock.Set(t0.Add(36 * time.Hour))
	p := e.add(t, "A", "B")
	if !p.JoinedAt.Equal(t0.Add(36 * time.Hour)) {
		t.Fatalf("joined_at = %v", p.JoinedAt)
	}
	got, _ := e.svc.Get(t.Context(), e.admin(), e.g1, p.ID)
	if !got.JoinedAt.Equal(p.JoinedAt) {
		t.Fatalf("relu %v", got.JoinedAt)
	}
}

func TestRegisterUsesVerificationInstant(t *testing.T) {
	e := newEnv(t)
	at := t0.Add(5 * time.Hour)
	p, err := e.svc.Register(t.Context(), e.g1, Input{FirstName: "A", Name: "B"}, at)
	if err != nil || !p.JoinedAt.Equal(at) {
		t.Fatalf("%v %v", p, err)
	}
}

func TestExportedRulesMatchManualAdd(t *testing.T) {
	e := newEnv(t)
	e.add(t, "Éloïse", "Dupont")
	e.add(t, "Zoé", "Noël")
	names := [][2]string{
		{"Éloïse", "Dupont"}, {"eloise", "DUPONT"}, {"ELOISE", "dupónt"}, {"Eloise", "Durand"},
		{"zoe", "noel"}, {"ZOÉ", "NOËL"}, {"Zoé", "Noel "}, {"Léa", "Noël"}, {" Zoé  ", "Noël"},
	}
	for _, n := range names {
		exists, err := NameExists(t.Context(), e.db, e.g1, n[0], n[1], 0)
		if err != nil {
			t.Fatal(err)
		}
		_, cerr := e.svc.Create(t.Context(), e.admin(), e.g1, Input{FirstName: n[0], Name: n[1]})
		if exists != (cerr != nil) {
			t.Errorf("%v : NameExists=%v mais ajout err=%v", n, exists, cerr)
		}
	}
	if NormalizeName("Éloïse", "DUPONT") != NormalizeName(" eloise ", "dupont") {
		t.Fatal("NormalizeName incohérent")
	}
	if NormalizeName("A", "B") == NormalizeName("A", "C") {
		t.Fatal("noms distincts confondus")
	}
}
