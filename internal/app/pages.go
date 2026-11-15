package app

import (
	"bytes"
	"fmt"
	"html/template"
	"net/http"

	"secretsanta/internal/accesspages"
	"secretsanta/internal/grouppages"
	"secretsanta/internal/i18n/catalog"
	i18nweb "secretsanta/internal/i18n/web"
	"secretsanta/internal/websec"
	"secretsanta/web"
)

// templateSet est un jeu de gabarits analysés une fois ; chaque rendu en prend
// un clone lié à la requête (jeton CSRF, langue).
type templateSet struct {
	tpl *template.Template
	cat *catalog.Catalog
}

// newTemplateSet analyse les dossiers de gabarits donnés (sous
// web/templates/), mise en page et sélecteur de langue en tête.
func newTemplateSet(cat *catalog.Catalog, dirs ...string) (*templateSet, error) {
	funcs := template.FuncMap{}
	for k, v := range websec.TemplateFuncsPlaceholder() {
		funcs[k] = v
	}
	for k, v := range i18nweb.FuncsPlaceholder() {
		funcs[k] = v
	}
	patterns := []string{"templates/layout/*.html", "templates/i18n/*.html"}
	for _, d := range dirs {
		patterns = append(patterns, "templates/"+d+"/*.html")
	}
	tpl, err := template.New("").Funcs(funcs).ParseFS(web.Templates, patterns...)
	if err != nil {
		return nil, fmt.Errorf("gabarits %v : %w", dirs, err)
	}
	return &templateSet{tpl: tpl, cat: cat}, nil
}

// render est un grouppages.Renderer : le gabarit name est exécuté sur un clone
// qui reçoit t, tn, csrfField et csrfToken de la requête ; la réponse est
// fabriquée en entier avant d'être écrite, jamais mise en cache.
func (s *templateSet) render(w http.ResponseWriter, r *http.Request, status int, name string, data any) {
	clone, err := s.tpl.Clone()
	if err != nil {
		http.Error(w, "Erreur interne.", http.StatusInternalServerError)
		return
	}
	clone.Funcs(websec.TemplateFuncs(r)).Funcs(i18nweb.Funcs(s.cat, r))
	var buf bytes.Buffer
	if err := clone.ExecuteTemplate(&buf, name, data); err != nil {
		http.Error(w, "Erreur interne.", http.StatusInternalServerError)
		return
	}
	h := w.Header()
	h.Set("Content-Type", "text/html; charset=utf-8")
	h.Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_, _ = w.Write(buf.Bytes())
}

// registerGroupPages branche la création de groupe et l'accueil d'un groupe
// sur Slugs. /creer est une route littérale : elle l'emporte sur /{slug}.
func registerGroupPages(a *App) error {
	set, err := newTemplateSet(a.Cat, "groups")
	if err != nil {
		return err
	}
	p, err := grouppages.New(grouppages.Deps{
		Groups: a.Groups, Render: set.render, DefaultLang: a.Cfg.DefaultLang,
		PublicURL: a.publicBase(), Location: a.Cfg.Location,
		AdminPath: func(slug string) string { return "/" + slug + "/admin/connexion" },
		ClientKey: clientIP,
	})
	if err != nil {
		return fmt.Errorf("pages des groupes : %w", err)
	}
	p.Register(a.Slugs)
	a.GroupPages = p
	return nil
}

// registerAccessPages branche les pages de connexion (participant et
// administrateur) sur Slugs ; le paquet analyse lui-même ses gabarits.
func registerAccessPages(a *App) error {
	p, err := accesspages.New(accesspages.Options{
		Groups: a.Groups, Credentials: a.Tokens, Magic: a.Magic, Sessions: a.Sessions,
		Translator: a.Cat, Clock: a.Clock, ClientIP: clientIP,
	})
	if err != nil {
		return fmt.Errorf("pages de connexion : %w", err)
	}
	p.Register(a.Slugs)
	a.AccessPages = p
	return nil
}
