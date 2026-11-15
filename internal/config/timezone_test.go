package config

import "testing"

func TestTimezoneDefault(t *testing.T) {
	c, err := Load(env(base()))
	if err != nil {
		t.Fatal(err)
	}
	if c.Timezone != "Europe/Paris" || c.Location.String() != "Europe/Paris" {
		t.Fatalf("fuseau par défaut : %s", c.Timezone)
	}
}

func TestTimezoneCustomAndInvalid(t *testing.T) {
	c, err := Load(env(with(VarTimezone, "America/Montreal")))
	if err != nil || c.Location.String() != "America/Montreal" {
		t.Fatalf("fuseau valide refusé : %v", err)
	}
	for _, bad := range []string{"Mars/Olympus", "Europe/Pariss", "Local", "+02:00", "../etc/passwd"} {
		mustFail(t, with(VarTimezone, bad), VarTimezone)
	}
}
