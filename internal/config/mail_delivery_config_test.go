package config

import "testing"

func TestMailSafeDefaults(t *testing.T) {
	c, err := Load(env(with(VarSMTPHost, "smtp.example.org", VarMailFrom, "noel@example.org")))
	if err != nil {
		t.Fatal(err)
	}
	if c.Mail.TLSMode != "starttls" || c.Mail.AllowInsecure {
		t.Fatalf("TLS par défaut : %+v", c.Mail)
	}
	if c.Mail.EnvelopeFrom != "noel@example.org" {
		t.Fatalf("expéditeur d'enveloppe : %q", c.Mail.EnvelopeFrom)
	}
	if c.Mail.MaxPerMinute != DefaultMaxPerMinute {
		t.Fatal("débit par défaut")
	}
}

func TestMailTLSNoneNeedsExplicitOptIn(t *testing.T) {
	msg := mustFail(t, with(VarMailTLSMode, "none"), VarMailTLSMode)
	mustFail(t, with(VarMailTLSMode, "none", VarMailAllowInsecure, "false"), VarMailAllowInsecure)
	_ = msg
	c, err := Load(env(with(VarMailTLSMode, "none", VarMailAllowInsecure, "true")))
	if err != nil || c.Mail.TLSMode != "none" {
		t.Fatalf("none explicite refusé : %v", err)
	}
	c, err = Load(env(with(VarMailTLSMode, "TLS")))
	if err != nil || c.Mail.TLSMode != "tls" {
		t.Fatalf("tls : %v", err)
	}
	mustFail(t, with(VarMailTLSMode, "ssl"), VarMailTLSMode)
	mustFail(t, with(VarMailAllowInsecure, "bof"), VarMailAllowInsecure)
}

func TestMailEHLO(t *testing.T) {
	for _, ok := range []string{"mail.example.org", "[203.0.113.7]", "[IPv6:2001:db8::1]"} {
		if _, err := Load(env(with(VarMailEHLOHostname, ok))); err != nil {
			t.Errorf("%q refusé : %v", ok, err)
		}
	}
	for _, bad := range []string{"localhost", "mail example.org", "203.0.113.7", "[pas-ip]", "-x.example.org", "mail.example.org\r\nRCPT"} {
		mustFail(t, with(VarMailEHLOHostname, bad), VarMailEHLOHostname)
	}
}

func TestMailOtherVariables(t *testing.T) {
	c, err := Load(env(with(VarSMTPHost, "h.example.org", VarMailFrom, "a@example.org", VarMailEnvelopeFrom, "b@example.org", VarMailReplyTo, "r@example.org", VarMailMaxPerMinute, "12")))
	if err != nil || c.Mail.EnvelopeFrom != "b@example.org" || c.Mail.ReplyTo != "r@example.org" || c.Mail.MaxPerMinute != 12 {
		t.Fatalf("%v %+v", err, c)
	}
	mustFail(t, with(VarMailEnvelopeFrom, "pas-une-adresse"), VarMailEnvelopeFrom)
	mustFail(t, with(VarMailReplyTo, "x"), VarMailReplyTo)
	mustFail(t, with(VarMailMaxPerMinute, "0"), VarMailMaxPerMinute)
	mustFail(t, with(VarMailMaxPerMinute, "abc"), VarMailMaxPerMinute)
}
