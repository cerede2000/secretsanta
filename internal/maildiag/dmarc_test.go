package maildiag

import (
	"context"
	"strings"
	"testing"
)

func TestDMARC(t *testing.T) {
	cases := []struct {
		name   string
		txt    []string
		absent bool
		status Status
		code   string
	}{
		{"absent", nil, true, StatusWarn, "dmarc.missing"},
		{"none", []string{"v=DMARC1; p=none"}, false, StatusOK, "dmarc.none"},
		{"quarantine", []string{"v=DMARC1; p=quarantine; pct=100"}, false, StatusOK, "dmarc.quarantine"},
		{"reject", []string{"v=DMARC1;p=reject"}, false, StatusOK, "dmarc.reject"},
		{"invalide", []string{"v=DMARC1; rua=mailto:x@example.org"}, false, StatusWarn, "dmarc.invalid"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := newResolver()
			if !c.absent {
				r.txt["_dmarc.example.org"] = c.txt
			}
			got := newDiag(baseConfig(), r).checkDMARC(context.Background(), "example.org")
			if got.Status != c.status || got.Code != c.code {
				t.Fatalf("obtenu %s/%s", got.Status, got.Code)
			}
			if c.absent {
				if got.Record == nil || got.Record.Name != "_dmarc.example.org" || !strings.HasPrefix(got.Record.Value, "v=DMARC1; p=none") {
					t.Fatalf("exemple: %+v", got.Record)
				}
			} else if got.Record != nil {
				t.Fatalf("aucun enregistrement attendu: %+v", got.Record)
			}
		})
	}
}
