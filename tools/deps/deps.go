//go:build tools

// Package deps sert uniquement à épingler les dépendances dans go.mod/go.sum
// avant les vagues de construction ; il sera retiré par la phase finale de livraison.
package deps

import (
	_ "github.com/emersion/go-msgauth/dkim"
	_ "github.com/makiuchi-d/gozxing"
	_ "github.com/makiuchi-d/gozxing/qrcode"
	_ "github.com/skip2/go-qrcode"
	_ "github.com/wneessen/go-mail"
	_ "golang.org/x/net/html"
	_ "golang.org/x/text/unicode/norm"
	_ "modernc.org/sqlite"
	_ "pgregory.net/rapid"
)
