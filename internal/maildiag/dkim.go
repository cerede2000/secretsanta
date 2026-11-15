package maildiag

import (
	"context"
	"crypto/ed25519"
	"crypto/x509"
	"encoding/base64"
	"strings"

	"secretsanta/internal/maildkim"
)

func (d *Diagnostic) checkDKIM(ctx context.Context) Check {
	c := d.cfg.DKIM
	if c.Domain == "" {
		return mk(CheckDKIM, StatusOff, "off", nil, nil)
	}
	name := c.Selector + "._domainkey." + c.Domain
	p := map[string]string{"domain": c.Domain, "selector": c.Selector}
	signer, err := maildkim.LoadKey(c.KeyFile, c.Algorithm)
	if err != nil {
		return mk(CheckDKIM, StatusError, "key_unreadable", p, nil)
	}
	var kind, pub string
	switch k := signer.Public().(type) {
	case ed25519.PublicKey:
		kind, pub = "ed25519", base64.StdEncoding.EncodeToString(k)
	default:
		der, err := x509.MarshalPKIXPublicKey(k)
		if err != nil {
			return mk(CheckDKIM, StatusError, "key_unreadable", p, nil)
		}
		kind, pub = "rsa", base64.StdEncoding.EncodeToString(der)
	}
	rec := &Record{Name: name, Type: "TXT", Value: "v=DKIM1; k=" + kind + "; p=" + pub}

	txt, err := d.lookupTXT(ctx, name)
	if err != nil {
		return mk(CheckDKIM, StatusWarn, "lookup_failed", p, nil)
	}
	var found bool
	for _, r := range txt {
		t := tags(r)
		if _, ok := t["p"]; !ok {
			continue
		}
		found = true
		k := strings.ToLower(t["k"])
		if k == "" {
			k = "rsa"
		}
		if k == kind && strings.Join(strings.Fields(t["p"]), "") == pub {
			return mk(CheckDKIM, StatusOK, "ok", p, nil)
		}
	}
	if !found {
		return mk(CheckDKIM, StatusWarn, "missing", p, rec)
	}
	return mk(CheckDKIM, StatusWarn, "mismatch", p, rec)
}
