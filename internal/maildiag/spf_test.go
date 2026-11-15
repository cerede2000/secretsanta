package maildiag

import (
	"context"
	"errors"
	"testing"
)

func TestSPF(t *testing.T) {
	cases := []struct {
		name    string
		txt     []string
		missing bool
		status  Status
		code    string
		record  bool
	}{
		{"absent", nil, true, StatusWarn, "spf.missing", true},
		{"autres TXT seulement", []string{"google-site-verification=x"}, false, StatusWarn, "spf.missing", true},
		{"plusieurs", []string{"v=spf1 mx -all", "v=spf1 a ~all"}, false, StatusError, "spf.multiple", true},
		{"-all", []string{"v=spf1 mx -all"}, false, StatusOK, "spf.hardfail", false},
		{"~all", []string{"v=spf1 mx ~all"}, false, StatusOK, "spf.softfail", false},
		{"+all", []string{"v=spf1 +all"}, false, StatusWarn, "spf.pass_all", true},
		{"all nu", []string{"v=spf1 all"}, false, StatusWarn, "spf.pass_all", true},
		{"?all", []string{"V=SPF1 mx ?all"}, false, StatusWarn, "spf.neutral_all", true},
		{"sans all", []string{"v=spf1 mx"}, false, StatusWarn, "spf.no_all", true},
		{"redirect", []string{"v=spf1 redirect=_spf.example.net"}, false, StatusOK, "spf.redirect", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := newResolver()
			if !c.missing {
				r.txt["example.org"] = c.txt
			}
			got := newDiag(baseConfig(), r).checkSPF(context.Background(), "example.org")
			if got.Status != c.status || got.Code != c.code {
				t.Fatalf("obtenu %s/%s, attendu %s/%s", got.Status, got.Code, c.status, c.code)
			}
			if (got.Record != nil) != c.record {
				t.Fatalf("enregistrement proposé: %v", got.Record)
			}
			if c.record && (got.Record.Type != "TXT" || got.Record.Name != "example.org" || got.Record.Value[:7] != "v=spf1 ") {
				t.Fatalf("enregistrement: %+v", got.Record)
			}
		})
	}
}

func TestSPFLookupFailure(t *testing.T) {
	r := newResolver()
	r.err = errors.New("timeout")
	got := newDiag(baseConfig(), r).checkSPF(context.Background(), "example.org")
	if got.Code != "spf.lookup_failed" || got.Status != StatusWarn {
		t.Fatalf("%+v", got)
	}
}
