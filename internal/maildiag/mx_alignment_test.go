package maildiag

import (
	"context"
	"net"
	"testing"

	"secretsanta/internal/config"
)

func TestMX(t *testing.T) {
	r := newResolver()
	d := newDiag(baseConfig(), r)
	if got := d.checkMX(context.Background(), "example.org"); got.Code != "mx.missing" || got.Status != StatusWarn {
		t.Fatalf("%+v", got)
	}
	r.mx["example.org"] = []*net.MX{{Host: "mx.example.org.", Pref: 10}}
	if got := d.checkMX(context.Background(), "example.org"); got.Code != "mx.ok" || got.Status != StatusOK {
		t.Fatalf("%+v", got)
	}
}

func TestAlignment(t *testing.T) {
	cases := []struct {
		name       string
		from, env  string
		dkim       string
		envStatus  Status
		dkimStatus Status
	}{
		{"tout aligné", "noel@example.org", "", "", StatusOK, StatusOff},
		{"enveloppe sous-domaine", "noel@example.org", "bounce@mail.example.org", "example.org", StatusOK, StatusOK},
		{"enveloppe désalignée", "noel@example.org", "bounce@fournisseur.net", "example.org", StatusWarn, StatusOK},
		{"dkim désaligné", "noel@example.org", "noel@example.org", "fournisseur.net", StatusOK, StatusWarn},
		{"les deux", "noel@example.org", "b@a.net", "c.net", StatusWarn, StatusWarn},
		{"domaine voisin", "noel@example.org", "b@notexample.org", "", StatusWarn, StatusOff},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			cfg := baseConfig()
			cfg.SMTP.From = c.from
			cfg.Mail.EnvelopeFrom = c.env
			cfg.DKIM = config.DKIM{Domain: c.dkim}
			checks := newDiag(cfg, newResolver()).checkAlignment()
			if len(checks) != 2 || checks[0].ID != CheckAlignEnv || checks[1].ID != CheckAlignDKIM {
				t.Fatalf("%+v", checks)
			}
			if checks[0].Status != c.envStatus || checks[1].Status != c.dkimStatus {
				t.Fatalf("env=%s dkim=%s", checks[0].Status, checks[1].Status)
			}
			if c.envStatus == StatusWarn && checks[0].Code != "alignment_envelope.misaligned" {
				t.Fatalf("code %s", checks[0].Code)
			}
		})
	}
}
