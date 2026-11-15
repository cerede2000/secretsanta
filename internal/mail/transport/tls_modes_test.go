package transport

import (
	"context"
	"testing"
)

// F6.AC2 : les trois modes TLS sont correctement transmis à la bibliothèque.
func TestTLSModes(t *testing.T) {
	for _, mode := range []string{"none", "starttls", "tls"} {
		t.Run(mode, func(t *testing.T) {
			s := newFakeServer(t, mode)
			o := baseOpts(s, mode)
			o.Mail.AllowInsecure = mode == "none"
			tr, err := New(o)
			if err != nil {
				t.Fatal(err)
			}
			if err := tr.Send(context.Background(), env()); err != nil {
				t.Fatalf("envoi : %v", err)
			}
			if len(s.data) != 1 {
				t.Fatalf("message non reçu")
			}
			if want := mode != "none"; s.tlsUsed[0] != want {
				t.Fatalf("canal chiffré = %v, attendu %v", s.tlsUsed[0], want)
			}
		})
	}
}

func TestSTARTTLSNeverDowngrades(t *testing.T) {
	s := newFakeServer(t, "starttls")
	s.noStart = true
	tr, _ := New(baseOpts(s, "starttls"))
	if err := tr.Send(context.Background(), env()); err == nil {
		t.Fatal("envoi en clair accepté sans STARTTLS")
	}
	if len(s.data) != 0 {
		t.Fatal("message transmis en clair")
	}
}

func TestUnknownModeRefused(t *testing.T) {
	s := newFakeServer(t, "none")
	if _, err := New(baseOpts(s, "ssl")); err == nil {
		t.Fatal("mode inconnu accepté")
	}
}
