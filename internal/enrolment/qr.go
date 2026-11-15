package enrolment

import (
	qrcode "github.com/skip2/go-qrcode"

	"secretsanta/internal/shared/errs"
)

// QRSize est le côté en pixels des QR codes produits.
const QRSize = 320

// QRCodePNG renvoie un PNG qui encode text. À servir depuis une adresse de la
// même origine (Content-Type image/png), jamais en data: URI.
func QRCodePNG(text string) ([]byte, error) {
	if text == "" {
		return nil, errs.Validation("Le texte du QR code est vide.")
	}
	b, err := qrcode.Encode(text, qrcode.Medium, QRSize)
	if err != nil {
		return nil, errs.Internal(err)
	}
	return b, nil
}
