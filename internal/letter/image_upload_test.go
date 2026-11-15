package letter

import (
	"bytes"
	"image/jpeg"
	"strings"
	"testing"

	"secretsanta/internal/shared/errs"
)

func TestImageUpload(t *testing.T) {
	e := newEnv(t)
	p := e.person(t, "Alice", "normal")
	ctx := t.Context()
	id, err := e.svc.AddIdea(ctx, e.group, p, IdeaInput{Title: "t", Image: pngBytes(t, 8, 8)})
	if err != nil {
		t.Fatal(err)
	}
	b, mime, err := e.svc.ImageFile(ctx, e.group, id)
	if err != nil || mime != "image/png" || !bytes.HasPrefix(b, []byte("\x89PNG")) {
		t.Fatalf("image locale attendue : %v %s", err, mime)
	}
	// Une image JPEG réelle est acceptée et reste JPEG.
	var jb bytes.Buffer
	_ = jpeg.Encode(&jb, mustImg(t), nil)
	id2, err := e.svc.AddIdea(ctx, e.group, p, IdeaInput{Title: "j", Image: jb.Bytes()})
	if err != nil {
		t.Fatal(err)
	}
	if _, mime, _ := e.svc.ImageFile(ctx, e.group, id2); mime != "image/jpeg" {
		t.Fatalf("jpeg attendu : %s", mime)
	}
	// Un autre groupe ne lit pas l'image.
	if _, _, err := e.svc.ImageFile(ctx, e.group+1, id); !errs.Is(err, errs.KindNotFound) {
		t.Fatalf("autre groupe : %v", err)
	}
	bad := map[string][]byte{
		"texte":          []byte("pas une image"),
		"html déguisé":   []byte("<html><script>alert(1)</script></html>"),
		"svg":            []byte(`<svg xmlns="http://www.w3.org/2000/svg"/>`),
		"entête tronqué": pngBytes(t, 8, 8)[:20],
		"trop lourde":    append(pngBytes(t, 8, 8), bytes.Repeat([]byte{0}, ImageMaxBytes)...),
		"trop grande":    pngBytes(t, ImageMaxSide+1, 1),
	}
	for name, data := range bad {
		if _, err := e.svc.AddIdea(ctx, e.group, p, IdeaInput{Title: "x", Image: data}); !errs.Is(err, errs.KindValidation) {
			t.Errorf("%s devrait être refusée : %v", name, err)
		}
	}
	// Le lien n'est jamais une image : une URL distante n'est ni stockée ni servie comme image.
	id3, err := e.svc.AddIdea(ctx, e.group, p, IdeaInput{Title: "lien", URL: "https://exemple.fr/photo.png"})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := e.svc.ImageFile(ctx, e.group, id3); !errs.Is(err, errs.KindNotFound) {
		t.Fatalf("aucune image pour un simple lien : %v", err)
	}
	l, _ := e.svc.Get(ctx, e.group, p)
	for _, i := range l.Ideas {
		if strings.Contains(i.Title, "http") {
			t.Fatal("inattendu")
		}
	}
	// Remplacement et retrait nettoient le fichier.
	if err := e.svc.UpdateIdea(ctx, e.group, p, id, IdeaInput{Title: "t", RemoveImage: true}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := e.svc.ImageFile(ctx, e.group, id); !errs.Is(err, errs.KindNotFound) {
		t.Fatalf("image retirée : %v", err)
	}
	// Conservation sans nouvelle image.
	if err := e.svc.UpdateIdea(ctx, e.group, p, id2, IdeaInput{Title: "j2"}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := e.svc.ImageFile(ctx, e.group, id2); err != nil {
		t.Fatalf("image conservée : %v", err)
	}
}
