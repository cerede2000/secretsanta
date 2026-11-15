package letter

import (
	"bytes"
	"image"
	"image/gif"
	"image/jpeg"
	"image/png"
	"net/url"
	"strings"

	"secretsanta/internal/shared/errs"
	"secretsanta/internal/shared/sanitize"
)

const (
	msgMaxIdeas  = "Une lettre contient au plus 5 idées : supprimez-en une avant d'en ajouter une autre."
	msgURL       = "Le lien doit commencer par http:// ou https:// et être une adresse valide."
	msgImageSize = "L'image est trop lourde (2 Mo au maximum)."
	msgImageType = "Ce fichier n'est pas une image valide (JPEG, PNG ou GIF)."
	msgImageDims = "L'image est trop grande (4000 pixels de côté au maximum)."
)

type cleanIdea struct {
	title, url, text string
}

func cleanInput(in IdeaInput) (cleanIdea, error) {
	var c cleanIdea
	var err error
	if c.title, err = sanitize.Required(in.Title, TitleMax, "Le titre"); err != nil {
		return c, err
	}
	if c.text, err = sanitize.Text(in.Text, TextMax, "Le texte"); err != nil {
		return c, err
	}
	if c.url, err = cleanURL(in.URL); err != nil {
		return c, err
	}
	return c, nil
}

func cleanURL(raw string) (string, error) {
	raw, err := sanitize.Line(raw, URLMax, "Le lien")
	if err != nil || raw == "" {
		return raw, err
	}
	if strings.ContainsAny(raw, " \\") {
		return "", errs.Validation(msgURL)
	}
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || u.Hostname() == "" || u.User != nil || u.Opaque != "" {
		return "", errs.Validation(msgURL)
	}
	if s := strings.ToLower(u.Scheme); s != "http" && s != "https" {
		return "", errs.Validation(msgURL)
	}
	return raw, nil
}

// checkImage vérifie la taille, le type réel (par décodage) et les dimensions,
// puis ré-encode l'image (métadonnées retirées). Renvoie contenu et type MIME.
func checkImage(data []byte) ([]byte, string, error) {
	if len(data) > ImageMaxBytes {
		return nil, "", errs.Validation(msgImageSize)
	}
	cfg, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return nil, "", errs.Validation(msgImageType)
	}
	if cfg.Width <= 0 || cfg.Height <= 0 || cfg.Width > ImageMaxSide || cfg.Height > ImageMaxSide {
		return nil, "", errs.Validation(msgImageDims)
	}
	img, format, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, "", errs.Validation(msgImageType)
	}
	var out bytes.Buffer
	switch format {
	case "jpeg":
		if err := jpeg.Encode(&out, img, &jpeg.Options{Quality: 85}); err != nil {
			return nil, "", errs.Internal(err)
		}
		return out.Bytes(), "image/jpeg", nil
	case "png", "gif":
		if err := png.Encode(&out, img); err != nil {
			return nil, "", errs.Internal(err)
		}
		return out.Bytes(), "image/png", nil
	}
	return nil, "", errs.Validation(msgImageType)
}

var _ = gif.Decode // enregistre le décodeur GIF
