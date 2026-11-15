package guard

import (
	"io/fs"
	"regexp"
	"strings"
	"testing"
	"unicode"

	"golang.org/x/net/html"

	"secretsanta/web"
)

// rawTextAllowList est la liste d'exceptions, volontairement vide : tout texte
// visible d'un gabarit passe par {{t ...}} ou {{tn ...}}. Pour ajouter une
// exception, ajouter une entrée "chemin du gabarit" -> textes exacts tolérés
// (après réduction des espaces) et justifier ici pourquoi la traduction est
// impossible (par exemple un nom propre de marque sans équivalent).
var rawTextAllowList = map[string][]string{}

// Le texte sans lettre (ponctuation, séparateur « · », chiffres, symboles) est
// toujours toléré : il n'y a rien à traduire.

// textAttrs : attributs dont la valeur est lue ou affichée par l'utilisateur.
var textAttrs = map[string]bool{
	"alt": true, "title": true, "placeholder": true, "aria-label": true,
	"aria-description": true, "aria-placeholder": true, "aria-roledescription": true,
	"aria-valuetext": true, "label": true,
}

var actionRe = regexp.MustCompile(`(?s)\{\{/\*.*?\*/\}\}|\{\{.*?\}\}`)

func hasLetter(s string) bool {
	for _, r := range s {
		if unicode.IsLetter(r) {
			return true
		}
	}
	return false
}

// rawTexts renvoie les textes bruts (nœuds texte et attributs lisibles) d'un
// gabarit, une fois les actions {{...}} et commentaires retirés.
func rawTexts(src string) []string {
	// Une action devient « § » : sans lettre, elle ne compte pas comme du texte.
	stripped := actionRe.ReplaceAllString(src, "§")
	var out []string
	z := html.NewTokenizer(strings.NewReader(stripped))
	skip := ""
	for {
		tt := z.Next()
		switch tt {
		case html.ErrorToken:
			return out
		case html.StartTagToken, html.SelfClosingTagToken:
			name, hasAttr := z.TagName()
			if tt == html.StartTagToken && (string(name) == "script" || string(name) == "style") {
				skip = string(name)
			}
			meta := string(name) == "meta"
			var metaName string
			var attrs [][2]string
			for hasAttr {
				var k, v []byte
				k, v, hasAttr = z.TagAttr()
				attrs = append(attrs, [2]string{string(k), string(v)})
				if string(k) == "name" || string(k) == "property" {
					metaName = string(v)
				}
			}
			for _, a := range attrs {
				switch {
				case textAttrs[a[0]]:
				case meta && a[0] == "content" && (metaName == "description" || strings.HasPrefix(metaName, "og:") || strings.HasPrefix(metaName, "twitter:")):
				case a[0] == "value" && string(name) == "input":
				default:
					continue
				}
				if hasLetter(a[1]) {
					out = append(out, normalizeSpaces(a[1]))
				}
			}
		case html.EndTagToken:
			name, _ := z.TagName()
			if string(name) == skip {
				skip = ""
			}
		case html.TextToken:
			if skip != "" {
				continue
			}
			if s := string(z.Text()); hasLetter(s) {
				out = append(out, normalizeSpaces(s))
			}
		}
	}
}

func normalizeSpaces(s string) string { return strings.Join(strings.Fields(s), " ") }

func allowed(file, text string) bool {
	for _, a := range rawTextAllowList[file] {
		if a == text {
			return true
		}
	}
	return false
}

func TestNoRawTextInTemplates(t *testing.T) {
	files := templateFiles(t)
	if len(files) == 0 {
		t.Fatal("aucun gabarit trouvé : le garde-fou ne vérifierait rien")
	}
	for _, f := range files {
		b, err := fs.ReadFile(web.Templates, f)
		if err != nil {
			t.Fatal(err)
		}
		for _, txt := range rawTexts(string(b)) {
			if !allowed(f, txt) {
				t.Errorf("%s : texte brut %q ; utiliser {{t \"module.clé\"}} (ou {{tn ...}})", f, txt)
			}
		}
	}
}

func TestRawTextDetector(t *testing.T) {
	bad := map[string]string{
		"text node":        `<p>Bonjour</p>`,
		"alt":              `<img src="x" alt="Logo">`,
		"aria-label":       `<nav aria-label="Menu">{{t "a.b"}}</nav>`,
		"placeholder":      `<input placeholder="Nom">`,
		"title attr":       `<a title="Aide">{{t "a.b"}}</a>`,
		"after action":     `<p>{{t "a.b"}} merci</p>`,
		"inside if":        `{{if .X}}<span>Oui</span>{{end}}`,
		"meta description": `<meta name="description" content="Un site">`,
		"submit value":     `<input type="submit" value="Envoyer">`,
	}
	for name, src := range bad {
		if len(rawTexts(src)) == 0 {
			t.Errorf("%s : texte brut non détecté dans %s", name, src)
		}
	}
	good := map[string]string{
		"t":            `<p>{{t "a.b"}}</p>`,
		"tn":           `<p>{{tn "a.b" .N "n" .N}}</p>`,
		"comment":      `{{/* Texte de commentaire */}}<p>{{t "a.b"}}</p>`,
		"separator":    `<title>{{t "a.b"}} · {{t "a.c"}}</title>`,
		"attr action":  `<a href="/x"{{if .Active}} aria-current="page"{{end}}>{{t .Label}}</a>`,
		"svg":          `<svg viewBox="0 0 24 24" aria-hidden="true"><path d="M2.5 20a6.5"/></svg>`,
		"script":       `<script type="module" src="/static/x.js"></script>`,
		"label attr":   `<input aria-label="{{t "a.b"}}" placeholder="{{t "a.c"}}">`,
		"digits":       `<span>12 / 34</span>`,
		"data attr":    `<div data-msg="{{t "a.b"}}" class="card"></div>`,
		"html comment": `<!-- Note --><p>{{t "a.b"}}</p>`,
	}
	for name, src := range good {
		if got := rawTexts(src); len(got) != 0 {
			t.Errorf("%s : faux positif %q dans %s", name, got, src)
		}
	}
}
