package maildiag

import (
	"context"
	"strings"
)

func (d *Diagnostic) checkMX(ctx context.Context, domain string) Check {
	p := map[string]string{"domain": domain}
	mx, err := d.res.LookupMX(ctx, domain)
	if err != nil && !isNotFound(err) {
		return mk(CheckMX, StatusWarn, "lookup_failed", p, nil)
	}
	if len(mx) == 0 {
		return mk(CheckMX, StatusWarn, "missing", p, nil)
	}
	return mk(CheckMX, StatusOK, "ok", p, nil)
}

// aligned applique l'alignement souple (DMARC relaxed) : domaines égaux ou
// l'un sous-domaine de l'autre.
func aligned(a, b string) bool {
	a, b = strings.TrimSuffix(strings.ToLower(a), "."), strings.TrimSuffix(strings.ToLower(b), ".")
	if a == "" || b == "" {
		return false
	}
	return a == b || strings.HasSuffix(a, "."+b) || strings.HasSuffix(b, "."+a)
}

func (d *Diagnostic) checkAlignment() []Check {
	from := addrDomain(d.cfg.SMTP.From)
	env := addrDomain(d.cfg.Mail.EnvelopeFrom)
	if env == "" {
		env = from
	}
	var out []Check
	pe := map[string]string{"from": from, "envelope": env}
	if aligned(from, env) {
		out = append(out, mk(CheckAlignEnv, StatusOK, "ok", pe, nil))
	} else {
		out = append(out, mk(CheckAlignEnv, StatusWarn, "misaligned", pe, nil))
	}
	if d.cfg.DKIM.Domain == "" {
		return append(out, mk(CheckAlignDKIM, StatusOff, "off", nil, nil))
	}
	pd := map[string]string{"from": from, "dkim": strings.ToLower(d.cfg.DKIM.Domain)}
	if aligned(from, d.cfg.DKIM.Domain) {
		return append(out, mk(CheckAlignDKIM, StatusOK, "ok", pd, nil))
	}
	return append(out, mk(CheckAlignDKIM, StatusWarn, "misaligned", pd, nil))
}
