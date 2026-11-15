package web

import (
	"bytes"
	"html/template"
	"io/fs"
	"strings"
	"testing"
)

func list(t *testing.T, fsys fs.FS, root string) []string {
	t.Helper()
	var out []string
	err := fs.WalkDir(fsys, root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() {
			out = append(out, p)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("parcours de %s : %v", root, err)
	}
	return out
}

func contains(files []string, want string) bool {
	for _, f := range files {
		if f == want {
			return true
		}
	}
	return false
}

func TestEmbedTemplates(t *testing.T) {
	files := list(t, Templates, "templates")
	for _, want := range []string{"templates/layout/base.html", "templates/layout/admin.html"} {
		if !contains(files, want) {
			t.Errorf("gabarit non embarqué : %s (trouvé : %v)", want, files)
		}
	}
}

func TestEmbedStaticAssets(t *testing.T) {
	files := list(t, Static, "static")
	for _, want := range []string{"static/css/base/base.css", "static/css/base/admin-nav.css"} {
		if !contains(files, want) {
			t.Errorf("asset non embarqué : %s", want)
		}
	}
}

func TestEmbedCatalogs(t *testing.T) {
	for _, lang := range []string{"fr", "en"} {
		for _, want := range []string{"static/i18n/core/" + lang + ".json", "static/i18n/layout/" + lang + ".json"} {
			if _, err := fs.Stat(I18n, want); err != nil {
				t.Errorf("catalogue non embarqué : %s : %v", want, err)
			}
			if _, err := fs.Stat(Static, want); err != nil {
				t.Errorf("catalogue absent de Static : %s : %v", want, err)
			}
		}
	}
}

// Les catalogues de profondeur deux (module/sous-dossier/langue.json) sont
// embarqués : layout/nav en est un exemple réel.
func TestEmbedCatalogsDepthTwo(t *testing.T) {
	for _, lang := range []string{"fr", "en"} {
		want := "static/i18n/layout/nav/" + lang + ".json"
		if !contains(list(t, I18n, "static/i18n"), want) {
			t.Errorf("catalogue de profondeur deux non embarqué : %s", want)
		}
	}
	m, err := fs.Glob(I18n, "static/i18n/*/*/*.json")
	if err != nil || len(m) == 0 {
		t.Errorf("Glob de profondeur deux vide : %v %v", m, err)
	}
}

func TestLayoutTemplatesRender(t *testing.T) {
	funcs := template.FuncMap{"t": func(k string, _ ...any) string { return "«" + k + "»" }}
	tpl, err := template.New("").Funcs(funcs).ParseFS(Templates, "templates/layout/*.html")
	if err != nil {
		t.Fatal(err)
	}
	type item struct {
		Destination, Label, Icon string
		Active                   bool
	}
	data := map[string]any{
		"Lang": "fr",
		"Nav": []item{
			{"/a", "layout.nav.dashboard", "dashboard", true},
			{"/b", "layout.nav.participants", "participants", false},
			{"/c", "layout.nav.draw", "draw", false},
			{"/d", "layout.nav.registrations", "registrations", false},
			{"/e", "layout.nav.more", "more", false},
		},
	}
	for _, name := range []string{"layout/home", "layout/base", "layout/admin"} {
		var b bytes.Buffer
		if err := tpl.ExecuteTemplate(&b, name, data); err != nil {
			t.Fatalf("%s : %v", name, err)
		}
		out := b.String()
		for _, banned := range []string{"style=", "<style", "<script", "data:", " onclick="} {
			if strings.Contains(out, banned) {
				t.Errorf("%s contient %q (CSP stricte)", name, banned)
			}
		}
		if !strings.Contains(out, "«layout.skip»") {
			t.Errorf("%s : texte non passé par t", name)
		}
	}
	var b bytes.Buffer
	_ = tpl.ExecuteTemplate(&b, "layout/admin", data)
	if n := strings.Count(b.String(), "<a class=\"admin-nav-item\""); n != 5 {
		t.Errorf("5 entrées attendues, %d trouvées", n)
	}
}
