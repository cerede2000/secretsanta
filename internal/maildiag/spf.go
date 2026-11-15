package maildiag

import (
	"context"
	"strings"
)

func spfExample(domain string) *Record {
	return &Record{Name: domain, Type: "TXT", Value: "v=spf1 mx include:spf.exemple-fournisseur.net ~all"}
}

func (d *Diagnostic) checkSPF(ctx context.Context, domain string) Check {
	p := map[string]string{"domain": domain}
	txt, err := d.lookupTXT(ctx, domain)
	if err != nil {
		return mk(CheckSPF, StatusWarn, "lookup_failed", p, nil)
	}
	recs := withPrefix(txt, "v=spf1")
	switch len(recs) {
	case 0:
		return mk(CheckSPF, StatusWarn, "missing", p, spfExample(domain))
	case 1:
	default:
		return mk(CheckSPF, StatusError, "multiple", p, spfExample(domain))
	}
	p["record"] = recs[0]
	final := ""
	redirect := false
	for _, tok := range strings.Fields(strings.ToLower(recs[0]))[1:] {
		switch {
		case tok == "all" || (len(tok) == 4 && strings.HasSuffix(tok, "all") && strings.ContainsAny(tok[:1], "+-~?")):
			final = tok
		case strings.HasPrefix(tok, "redirect="):
			redirect = true
		}
	}
	switch final {
	case "-all":
		return mk(CheckSPF, StatusOK, "hardfail", p, nil)
	case "~all":
		return mk(CheckSPF, StatusOK, "softfail", p, nil)
	case "+all", "all":
		return mk(CheckSPF, StatusWarn, "pass_all", p, spfExample(domain))
	case "?all":
		return mk(CheckSPF, StatusWarn, "neutral_all", p, spfExample(domain))
	}
	if redirect {
		return mk(CheckSPF, StatusOK, "redirect", p, nil)
	}
	return mk(CheckSPF, StatusWarn, "no_all", p, spfExample(domain))
}
