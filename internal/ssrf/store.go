package ssrf

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// ErrImageNotFound signale un nom de copie locale inconnu ou invalide.
var ErrImageNotFound = errors.New("ssrf: image locale introuvable")

// localName : empreinte SHA-256 du contenu et extension d'un type autorisé.
var localName = regexp.MustCompile(`^[0-9a-f]{64}\.(png|jpg|gif)$`)

var extTypes = map[string]string{"png": "image/png", "jpg": "image/jpeg", "gif": "image/gif"}

// Store garde les copies locales des images vérifiées, nommées par
// l'empreinte de leur contenu, dans un dossier du volume de données.
type Store struct {
	dir    string
	prefix string
}

// LocalImage décrit une copie locale. Elle ne contient aucune adresse
// externe : Src est un chemin de même origine.
type LocalImage struct {
	Name        string
	ContentType string
	Size        int
	Width       int
	Height      int
	// Src est le chemin local à mettre dans l'attribut src (prefix/Name).
	Src string
}

// NewStore ouvre (et crée au besoin) le dossier dir ; urlPrefix est le
// chemin de même origine sous lequel Handler est monté (ex. "/images").
func NewStore(dir, urlPrefix string) (*Store, error) {
	if dir == "" {
		return nil, errors.New("ssrf: dossier des images manquant")
	}
	p := strings.TrimRight(urlPrefix, "/")
	if !strings.HasPrefix(p, "/") || strings.HasPrefix(p, "//") || strings.ContainsAny(p, ":\\?#") {
		return nil, fmt.Errorf("ssrf: préfixe local invalide %q", urlPrefix)
	}
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return nil, err
	}
	return &Store{dir: dir, prefix: p}, nil
}

// Save écrit une image vérifiée. Le contenu est revérifié : une Image
// construite à la main ne passe pas sans être une vraie image.
func (s *Store) Save(img *Image) (LocalImage, error) {
	if img == nil {
		return LocalImage{}, ErrNotImage
	}
	checked, err := CheckImage(img.Data)
	if err != nil {
		return LocalImage{}, err
	}
	sum := sha256.Sum256(checked.Data)
	name := hex.EncodeToString(sum[:]) + "." + checked.Ext
	final := filepath.Join(s.dir, name)
	li := LocalImage{
		Name: name, ContentType: checked.ContentType, Size: len(checked.Data),
		Width: checked.Width, Height: checked.Height, Src: s.prefix + "/" + name,
	}
	// Nom = empreinte du contenu : un fichier déjà présent et identique suffit.
	if existing, err := os.ReadFile(final); err == nil && bytes.Equal(existing, checked.Data) {
		return li, nil
	}
	tmp, err := os.CreateTemp(s.dir, ".tmp-*")
	if err != nil {
		return LocalImage{}, err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(checked.Data); err != nil {
		tmp.Close()
		return LocalImage{}, err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return LocalImage{}, err
	}
	if err := tmp.Close(); err != nil {
		return LocalImage{}, err
	}
	if err := os.Chmod(tmp.Name(), 0o640); err != nil {
		return LocalImage{}, err
	}
	if err := os.Rename(tmp.Name(), final); err != nil {
		return LocalImage{}, err
	}
	return li, nil
}

// Copy récupère l'image par la garde, la vérifie sur son contenu et la copie
// en local. Seul le LocalImage renvoyé doit être affiché.
func (s *Store) Copy(ctx context.Context, g *Guard, rawURL string) (LocalImage, error) {
	img, err := g.FetchImage(ctx, rawURL)
	if err != nil {
		return LocalImage{}, err
	}
	return s.Save(img)
}

// Open ouvre une copie locale par son nom et renvoie son type.
func (s *Store) Open(name string) (*os.File, string, error) {
	m := localName.FindStringSubmatch(name)
	if m == nil {
		return nil, "", ErrImageNotFound
	}
	f, err := os.Open(filepath.Join(s.dir, name))
	if err != nil {
		return nil, "", ErrImageNotFound
	}
	return f, extTypes[m[1]], nil
}

// IsLocalSrc dit si src est un chemin de même origine servi par ce magasin
// (jamais une adresse externe, jamais une ressource data:).
func (s *Store) IsLocalSrc(src string) bool {
	rest, ok := strings.CutPrefix(src, s.prefix+"/")
	return ok && localName.MatchString(rest)
}

// Handler sert les copies locales en GET et HEAD. Il attend le nom seul
// comme chemin : le monter avec http.StripPrefix(prefix, …).
func (s *Store) Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
			return
		}
		f, ct, err := s.Open(strings.TrimPrefix(r.URL.Path, "/"))
		if err != nil {
			http.NotFound(w, r)
			return
		}
		defer f.Close()
		st, err := f.Stat()
		if err != nil || !st.Mode().IsRegular() {
			http.NotFound(w, r)
			return
		}
		h := w.Header()
		h.Set("Content-Type", ct)
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Content-Security-Policy", "default-src 'none'; sandbox")
		h.Set("Cache-Control", "public, max-age=31536000, immutable")
		http.ServeContent(w, r, "", st.ModTime(), f)
	})
}
