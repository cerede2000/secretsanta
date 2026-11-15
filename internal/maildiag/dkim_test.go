package maildiag

import (
	"context"
	"crypto/x509"
	"encoding/base64"
	"strings"
	"testing"

	"secretsanta/internal/config"
)

func dkimConfig(path string) *config.Config {
	c := baseConfig()
	c.DKIM = config.DKIM{Domain: "example.org", Selector: "mail", KeyFile: config.NewSecret(path), Algorithm: config.AlgRSA}
	return c
}

func TestDKIMDisabled(t *testing.T) {
	got := newDiag(baseConfig(), newResolver()).checkDKIM(context.Background())
	if got.Status != StatusOff || got.Code != "dkim.off" || got.Record != nil {
		t.Fatalf("%+v", got)
	}
}

func TestDKIMEnabled(t *testing.T) {
	path, key := writeRSAKey(t)
	der, _ := x509.MarshalPKIXPublicKey(&key.PublicKey)
	pub := base64.StdEncoding.EncodeToString(der)
	name := "mail._domainkey.example.org"
	cases := []struct {
		name   string
		txt    []string
		absent bool
		code   string
		status Status
	}{
		{"correspond", []string{"v=DKIM1; k=rsa; p=" + pub}, false, "dkim.ok", StatusOK},
		{"correspond coupé", []string{"v=DKIM1; p=" + pub[:40] + " " + pub[40:]}, false, "dkim.ok", StatusOK},
		{"écart", []string{"v=DKIM1; k=rsa; p=AAAA"}, false, "dkim.mismatch", StatusWarn},
		{"absent", nil, true, "dkim.missing", StatusWarn},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := newResolver()
			if !c.absent {
				r.txt[name] = c.txt
			}
			got := newDiag(dkimConfig(path), r).checkDKIM(context.Background())
			if got.Code != c.code || got.Status != c.status {
				t.Fatalf("obtenu %s/%s", got.Status, got.Code)
			}
			if c.status == StatusWarn {
				if got.Record == nil || got.Record.Name != name || !strings.Contains(got.Record.Value, "p="+pub) || !strings.HasPrefix(got.Record.Value, "v=DKIM1; k=rsa;") {
					t.Fatalf("enregistrement: %+v", got.Record)
				}
			} else if got.Record != nil {
				t.Fatalf("pas d'enregistrement attendu")
			}
		})
	}
}

func TestDKIMUnreadableKey(t *testing.T) {
	got := newDiag(dkimConfig("/nonexistent/key.pem"), newResolver()).checkDKIM(context.Background())
	if got.Code != "dkim.key_unreadable" || got.Status != StatusError {
		t.Fatalf("%+v", got)
	}
	for _, v := range got.Params {
		if strings.Contains(v, "nonexistent") {
			t.Fatal("le chemin de la clé ne doit pas fuir")
		}
	}
}
