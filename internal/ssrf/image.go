package ssrf

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"image/gif"
	"image/jpeg"
	"image/png"
	"mime"
	"strings"
)

// Limites des images acceptées.
const (
	// MaxImageDimension est la largeur ou hauteur maximale acceptée.
	MaxImageDimension = 5000
	// MaxImagePixels plafonne largeur × hauteur (bombe de décompression).
	MaxImagePixels = 12_000_000
)

// ErrNotImage signale un contenu qui n'est pas une image d'un type autorisé.
var ErrNotImage = errors.New("ssrf: contenu refusé (pas une image JPEG, PNG ou GIF valide)")

// Image est une image vérifiée sur son contenu. Elle ne garde pas son
// adresse d'origine : seule sa copie locale peut être affichée.
type Image struct {
	// ContentType est déduit du contenu, jamais de l'en-tête reçu.
	ContentType string
	// Ext est l'extension de fichier sans point (png, jpg, gif).
	Ext    string
	Width  int
	Height int
	Data   []byte
}

type imageFormat struct {
	contentType string
	ext         string
	magic       [][]byte
	config      func([]byte) (image.Config, error)
	decode      func([]byte) error
}

var imageFormats = []imageFormat{
	{
		contentType: "image/png", ext: "png",
		magic:  [][]byte{[]byte("\x89PNG\r\n\x1a\n")},
		config: func(b []byte) (image.Config, error) { return png.DecodeConfig(bytes.NewReader(b)) },
		decode: func(b []byte) error { _, err := png.Decode(bytes.NewReader(b)); return err },
	},
	{
		contentType: "image/jpeg", ext: "jpg",
		magic:  [][]byte{[]byte("\xff\xd8\xff")},
		config: func(b []byte) (image.Config, error) { return jpeg.DecodeConfig(bytes.NewReader(b)) },
		decode: func(b []byte) error { _, err := jpeg.Decode(bytes.NewReader(b)); return err },
	},
	{
		contentType: "image/gif", ext: "gif",
		magic:  [][]byte{[]byte("GIF87a"), []byte("GIF89a")},
		config: func(b []byte) (image.Config, error) { return gif.DecodeConfig(bytes.NewReader(b)) },
		decode: func(b []byte) error { _, err := gif.Decode(bytes.NewReader(b)); return err },
	},
}

// CheckImage vérifie que data est réellement une image JPEG, PNG ou GIF :
// signature, dimensions bornées puis décodage complet. SVG, WebP et tout
// autre contenu sont refusés.
func CheckImage(data []byte) (*Image, error) {
	for _, f := range imageFormats {
		if !hasAnyPrefix(data, f.magic) {
			continue
		}
		cfg, err := f.config(data)
		if err != nil {
			return nil, fmt.Errorf("%w: %v", ErrNotImage, err)
		}
		if cfg.Width < 1 || cfg.Height < 1 || cfg.Width > MaxImageDimension || cfg.Height > MaxImageDimension ||
			cfg.Width*cfg.Height > MaxImagePixels {
			return nil, fmt.Errorf("%w: dimensions %dx%d", ErrNotImage, cfg.Width, cfg.Height)
		}
		if err := f.decode(data); err != nil {
			return nil, fmt.Errorf("%w: %v", ErrNotImage, err)
		}
		return &Image{ContentType: f.contentType, Ext: f.ext, Width: cfg.Width, Height: cfg.Height, Data: data}, nil
	}
	return nil, ErrNotImage
}

func hasAnyPrefix(b []byte, prefixes [][]byte) bool {
	for _, p := range prefixes {
		if bytes.HasPrefix(b, p) {
			return true
		}
	}
	return false
}

// declaredImage refuse un en-tête Content-Type qui annonce autre chose
// qu'une image ; l'absence d'en-tête ou application/octet-stream sont tolérés,
// le contenu tranche de toute façon.
func declaredImage(ct string) bool {
	if strings.TrimSpace(ct) == "" {
		return true
	}
	mt, _, err := mime.ParseMediaType(ct)
	if err != nil {
		return false
	}
	return strings.HasPrefix(mt, "image/") && mt != "image/svg+xml" || mt == "application/octet-stream"
}

// FetchImage récupère une image (plafonnée à MaxImageBytes) et la vérifie
// sur son contenu.
func (g *Guard) FetchImage(ctx context.Context, rawURL string) (*Image, error) {
	resp, err := g.fetch(ctx, rawURL, g.maxImageBytes, "image/png,image/jpeg,image/gif;q=0.9")
	if err != nil {
		return nil, err
	}
	if !declaredImage(resp.ContentType) {
		return nil, fmt.Errorf("%w: type annoncé %q", ErrNotImage, resp.ContentType)
	}
	return CheckImage(resp.Body)
}
