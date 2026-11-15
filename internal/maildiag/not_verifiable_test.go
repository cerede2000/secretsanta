package maildiag

import (
	"context"
	"testing"
)

func TestReportDeclaresNotVerifiable(t *testing.T) {
	rep := newDiag(baseConfig(), newResolver()).Run(context.Background())
	got := map[string]string{}
	for _, n := range rep.NotVerifiable {
		got[n.ID] = n.Guide
	}
	for _, id := range []string{"ip_reputation", "blocklists", "reverse_dns"} {
		g, ok := got[id]
		if !ok {
			t.Fatalf("%s manquant : %v", id, got)
		}
		if g != GuideAnchor || g == "" {
			t.Fatalf("renvoi au guide absent pour %s", id)
		}
	}
	if len(got) != 3 {
		t.Fatalf("%v", got)
	}
}
