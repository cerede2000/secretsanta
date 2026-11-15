package web

import (
	"bytes"
	"html/template"
	"io/fs"
	"regexp"
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
		for _, v := range cspViolations(out) {
			t.Errorf("%s : %s (CSP stricte)", name, v)
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

var (
	scriptRe  = regexp.MustCompile(`(?is)<script\b([^>]*)>(.*?)</script>`)
	scriptSrc = regexp.MustCompile(`(?i)\bsrc="(/static/[^"]*)"`)
	onAttrRe  = regexp.MustCompile(`(?i)\son[a-z]+\s*=`)
)

// cspViolations liste ce que la CSP du projet interdit : style en ligne, data:,
// URL externe, gestionnaires on*, script en ligne ou dont le src n'est pas
// local sous /static/. Un script sans contenu avec src="/static/..." passe.
func cspViolations(out string) []string {
	var v []string
	for _, banned := range []string{"style=", "<style", "data:", "http://", "https://"} {
		if strings.Contains(out, banned) {
			v = append(v, "contient "+banned)
		}
	}
	if onAttrRe.MatchString(out) {
		v = append(v, "gestionnaire on*")
	}
	rest := scriptRe.ReplaceAllStringFunc(out, func(tag string) string {
		m := scriptRe.FindStringSubmatch(tag)
		if strings.TrimSpace(m[2]) != "" {
			v = append(v, "script avec contenu")
		} else if !scriptSrc.MatchString(m[1]) {
			v = append(v, "script sans src local /static/")
		}
		return ""
	})
	if strings.Contains(strings.ToLower(rest), "<script") {
		v = append(v, "balise script mal formée")
	}
	return v
}

func TestCSPViolations(t *testing.T) {
	ok := `<script type="module" src="/static/js/theme/snow.js"></script>`
	if v := cspViolations(ok); len(v) != 0 {
		t.Errorf("script local refusé : %v", v)
	}
	for name, bad := range map[string]string{
		"en ligne":         `<script>alert(1)</script>`,
		"src externe":      `<script type="module" src="https://exemple.org/a.js"></script>`,
		"contenu et src":   `<script src="/static/js/a.js">alert(1)</script>`,
		"src relatif":      `<script src="a.js"></script>`,
		"gestionnaire on*": `<a href="/" onclick="x()">a</a>`,
	} {
		if len(cspViolations(bad)) == 0 {
			t.Errorf("%s : doit être refusé", name)
		}
	}
}
