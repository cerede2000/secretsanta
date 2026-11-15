package maildiag

import "context"

func (d *Diagnostic) checkDMARC(ctx context.Context, domain string) Check {
	name := "_dmarc." + domain
	p := map[string]string{"domain": domain}
	txt, err := d.lookupTXT(ctx, name)
	if err != nil {
		return mk(CheckDMARC, StatusWarn, "lookup_failed", p, nil)
	}
	recs := withPrefix(txt, "v=dmarc1")
	if len(recs) == 0 {
		return mk(CheckDMARC, StatusWarn, "missing", p,
			&Record{Name: name, Type: "TXT", Value: "v=DMARC1; p=none; rua=mailto:dmarc@" + domain})
	}
	if len(recs) > 1 {
		return mk(CheckDMARC, StatusError, "multiple", p, nil)
	}
	p["record"] = recs[0]
	switch pol := tags(recs[0])["p"]; pol {
	case "none", "quarantine", "reject":
		// p=none est une phase d'observation : correct, sans protection active.
		return mk(CheckDMARC, StatusOK, pol, p, nil)
	}
	return mk(CheckDMARC, StatusWarn, "invalid", p, nil)
}
