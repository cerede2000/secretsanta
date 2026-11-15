package enrolment

import (
	"testing"

	"secretsanta/internal/participants"
)

func TestPersonalLinkQRDecodesToSameLink(t *testing.T) {
	e := newEnv(t)
	c, err := e.svc.CreateVerified(t.Context(), e.gid, participants.Input{FirstName: "Léa", Name: "Martin"}, t0)
	if err != nil {
		t.Fatal(err)
	}
	if c.Link == "" {
		t.Fatal("lien absent")
	}
	b, err := QRCodePNG(c.Link)
	if err != nil {
		t.Fatal(err)
	}
	if got := decodeQR(t, b); got != c.Link {
		t.Fatalf("QR décodé %q, lien %q", got, c.Link)
	}
}
