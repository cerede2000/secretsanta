package enrolment

import "testing"

func TestQRCodePNGDecodable(t *testing.T) {
	for _, txt := range []string{
		"https://exemple.test/noel/acces/AbCdEfGhIjKlMnOpQrStUv",
		"https://exemple.test/noel/rejoindre/jeton-d-inscription_123",
	} {
		b, err := QRCodePNG(txt)
		if err != nil {
			t.Fatal(err)
		}
		if got := decodeQR(t, b); got != txt {
			t.Fatalf("décodé %q, attendu %q", got, txt)
		}
		// Entrées fixes : la chaîne complète de détection d'image reste vérifiée.
		if got := decodeQRDetected(t, b); got != txt {
			t.Fatalf("détecté %q, attendu %q", got, txt)
		}
	}
	if _, err := QRCodePNG(""); err == nil {
		t.Fatal("texte vide accepté")
	}
}
