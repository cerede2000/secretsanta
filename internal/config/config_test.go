package config

import (
	"strings"
	"testing"
)

func TestMissingSecretsRefused(t *testing.T) {
	for _, name := range RequiredVariables {
		m := base()
		delete(m, name)
		msg := mustFail(t, m, name)
		if !strings.Contains(msg, "obligatoire") {
			t.Errorf("message non français : %s", msg)
		}
	}
	_, err := Load(env(map[string]string{}))
	if err == nil || !strings.Contains(err.Error(), VarInstancePassword) || !strings.Contains(err.Error(), VarSigningSecret) {
		t.Fatalf("les deux variables doivent être nommées : %v", err)
	}
}

func TestStartsWithSecretsAndDefaults(t *testing.T) {
	c, err := Load(env(base()))
	if err != nil {
		t.Fatal(err)
	}
	if c.InstancePass.Reveal() != "mdp-instance-xyz" || c.SigningKey.Reveal() != "signature-secrete-abc" {
		t.Fatal("secrets mal lus")
	}
	if c.SMTP.Enabled() || c.DKIM.Enabled() || c.Sources.SearxngURL != "" {
		t.Fatal("SMTP, DKIM et sources doivent être désactivés par défaut")
	}
	if !c.RemindersEnabled || c.ListenAddr == "" || c.DataDir == "" {
		t.Fatal("défauts manquants")
	}
}

func TestSMTPOptionalButChecked(t *testing.T) {
	c, err := Load(env(with(VarSMTPHost, "smtp.example.org", VarMailFrom, "noel@example.org", VarSMTPUser, "u", VarSMTPPassword, "p")))
	if err != nil || !c.SMTP.Enabled() || c.SMTP.Port != 587 {
		t.Fatalf("SMTP valide refusé : %v", err)
	}
	mustFail(t, with(VarSMTPHost, "smtp.example.org"), VarMailFrom)
	mustFail(t, with(VarSMTPHost, "smtp.example.org", VarMailFrom, "noel@example.org", VarSMTPPort, "99999"), VarSMTPPort)
	mustFail(t, with(VarSMTPHost, "smtp.example.org", VarMailFrom, "noel@example.org", VarSMTPUser, "u"), VarSMTPPassword)
}

func TestOtherValidation(t *testing.T) {
	mustFail(t, with(VarPublicURL, "pas une url"), VarPublicURL)
	mustFail(t, with(VarRemindersEnabled, "peut-etre"), VarRemindersEnabled)
	mustFail(t, with(VarSearxngURL, "ftp://x"), VarSearxngURL)
	c, err := Load(env(with(VarSearxngURL, "http://searx:8080/", VarBraveAPIKey, "k")))
	if err != nil || c.Sources.SearxngURL != "http://searx:8080" || !c.Sources.BraveKey.IsSet() {
		t.Fatalf("sources : %v", err)
	}
}

func TestUndeclaredVariablePanics(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("lire une variable non déclarée doit échouer")
		}
	}()
	(&loader{getenv: env(nil), declared: map[string]bool{}}).get("NOPE")
}
