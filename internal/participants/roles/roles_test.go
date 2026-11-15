package roles

import (
	"testing"

	p "secretsanta/internal/participants"
)

var all = []p.DrawRole{p.RoleNormal, p.RoleReceptionSeule, p.RoleOffrePlus, p.RoleUnSeul}

func TestAdminAnyValue(t *testing.T) {
	for _, r := range all {
		if AdminMayChange(r) != Allowed {
			t.Fatalf("admin refusé pour %s", r)
		}
	}
	if v := AdminMayChange("x"); v != UnknownRole || v.Explain() == "" {
		t.Fatal("valeur inconnue acceptée")
	}
}

func TestSelfRules(t *testing.T) {
	if SelfMayChange(p.RoleNormal, p.RoleUnSeul, false) != Allowed ||
		SelfMayChange(p.RoleUnSeul, p.RoleNormal, false) != Allowed {
		t.Fatal("bascule normal/un_seul refusée")
	}
	for _, from := range all {
		for _, to := range all {
			if SelfMayChange(from, to, true) != LockedAfterDraw {
				t.Fatalf("pas verrouillé après tirage %s->%s", from, to)
			}
		}
	}
	for _, from := range []p.DrawRole{p.RoleReceptionSeule, p.RoleOffrePlus} {
		if v := SelfMayChange(from, p.RoleNormal, false); v != SetByAdmin || v.Explain() == "" {
			t.Fatalf("%s a pu se changer", from)
		}
	}
	for _, to := range []p.DrawRole{p.RoleReceptionSeule, p.RoleOffrePlus} {
		if SelfMayChange(p.RoleNormal, to, false) != SelfValueOnly {
			t.Fatalf("auto-choix de %s accepté", to)
		}
	}
	if Allowed.Explain() != "" {
		t.Fatal("explication pour un accord")
	}
}

func TestCounts(t *testing.T) {
	c := Counts([]p.DrawRole{p.RoleNormal, p.RoleNormal, p.RoleReceptionSeule, p.RoleOffrePlus, p.RoleUnSeul})
	want := p.RoleCounts{Beneficiaries: 5, Givers: 4, ReceptionSeule: 1, OffrePlus: 1, Normal: 2, UnSeul: 1}
	if c != want {
		t.Fatalf("got %+v want %+v", c, want)
	}
	if Counts(nil) != (p.RoleCounts{}) {
		t.Fatal("liste vide")
	}
}
