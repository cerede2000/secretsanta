package maildiag

import (
	"context"
	"net/http"
	"testing"
)

func TestRunDisabledMail(t *testing.T) {
	cfg := baseConfig()
	cfg.SMTP.Host = ""
	rep := New(Options{Config: cfg, Resolver: newResolver(), Client: &http.Client{Transport: failTransport{}}}).Run(context.Background())
	if c := mustCheck(t, rep, "mail"); c.Code != "mail.disabled" || c.Status != StatusOff {
		t.Fatalf("%+v", c)
	}
	if _, ok := rep.Check(CheckSPF); ok {
		t.Fatal("pas de contrôle DNS sans courrier configuré")
	}
}

func TestRunFullReport(t *testing.T) {
	r := newResolver()
	r.txt["example.org"] = []string{"v=spf1 mx -all"}
	rep := New(Options{Config: baseConfig(), Resolver: r, Client: &http.Client{Transport: failTransport{}}}).Run(context.Background())
	for _, id := range []string{CheckSPF, CheckDKIM, CheckDMARC, CheckMX, CheckAlignEnv, CheckAlignDKIM, CheckSMTPTLS, CheckPublicHealth} {
		mustCheck(t, rep, id)
	}
	if mustCheck(t, rep, CheckSPF).Status != StatusOK || mustCheck(t, rep, CheckDMARC).Status != StatusWarn {
		t.Fatal("statuts inattendus")
	}
}
