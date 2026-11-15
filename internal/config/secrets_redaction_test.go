package config

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"testing"
)

func TestNoSecretLeaks(t *testing.T) {
	keyPath := writeKey(t, fakePEM)
	m := with(
		VarSMTPHost, "smtp.example.org", VarMailFrom, "noel@example.org",
		VarSMTPUser, "utilisateur", VarSMTPPassword, "SMTPPASS-123",
		VarMailDKIMDomain, "example.org", VarMailDKIMSelector, "noel",
		VarMailDKIMKeyFile, keyPath, VarMailDKIMAlgorithm, "rsa-sha256",
		VarBraveAPIKey, "BRAVEKEY-456", VarSerperAPIKey, "SERPERKEY-789",
	)
	c, err := Load(env(m))
	if err != nil {
		t.Fatal(err)
	}
	secrets := []string{"SMTPPASS-123", "TOPSECRETKEYMATERIAL", keyPath, "BRAVEKEY-456", "SERPERKEY-789", "mdp-instance-xyz", "signature-secrete-abc"}

	var buf bytes.Buffer
	lg := slog.New(slog.NewTextHandler(&buf, nil))
	lg.Info("démarrage", "config", c)
	lg.Info("démarrage", "smtp", c.SMTP, "dkim", c.DKIM, "pass", c.SMTP.Password)
	js, _ := json.Marshal(c)
	jl := slog.New(slog.NewJSONHandler(&buf, nil))
	jl.Info("x", "config", c, "dkim", c.DKIM)

	outputs := []string{
		c.String(), fmt.Sprint(c), fmt.Sprintf("%v %+v %#v %s", c, c, c, c), fmt.Sprintf("%+v %#v", c.SMTP, c.DKIM),
		fmt.Sprintf("%v %+v %#v", c.SMTP.Password, c.InstancePass, c.SigningKey), buf.String(), string(js),
	}
	for _, out := range outputs {
		for _, s := range secrets {
			if strings.Contains(out, s) {
				t.Errorf("secret %q divulgué dans : %s", s, out)
			}
		}
	}
	if c.SMTP.Password.Reveal() != "SMTPPASS-123" {
		t.Fatal("Reveal doit rendre la valeur")
	}
}

func TestErrorsNeverContainSecrets(t *testing.T) {
	keyPath := writeKey(t, "pas une cle SECRETCONTENT")
	m := with(
		VarSMTPHost, "smtp.example.org", VarMailFrom, "invalide", VarSMTPPassword, "SMTPPASS-123",
		VarMailDKIMDomain, "example.org", VarMailDKIMSelector, "noel",
		VarMailDKIMKeyFile, keyPath, VarMailDKIMAlgorithm, "rsa-sha1",
		VarMailTLSMode, "none", "MAIL_DKIM_PRIVATE_KEY", fakePEM,
		VarBraveAPIKey, "BRAVEKEY-456", VarSearxngURL, "ftp://BRAVEKEY-456",
	)
	delete(m, VarSigningSecret)
	_, err := Load(env(m))
	if err == nil {
		t.Fatal("erreur attendue")
	}
	for _, s := range []string{"SMTPPASS-123", "SECRETCONTENT", keyPath, "TOPSECRETKEYMATERIAL", "mdp-instance-xyz", "BRAVEKEY-456"} {
		if strings.Contains(err.Error(), s) {
			t.Errorf("secret %q dans l'erreur : %v", s, err)
		}
	}
}
