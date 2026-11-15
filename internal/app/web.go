package app

import (
	"bytes"
	"fmt"
	"html/template"
	"io/fs"
	"net/http"
	"net/url"
	"strings"

	"secretsanta/internal/i18n/catalog"
	"secretsanta/internal/shared/adminnav"
	"secretsanta/internal/websec"
	"secretsanta/web"
)

// pages porte les gabarits analysés ; chaque requête en reçoit un clone lié à
// sa langue et à son jeton CSRF.
type pages struct {
	tpl *template.Template
	cat *catalog.Catalog
}

// newPages analyse les gabarits de mise en page avec des fonctions de repli ;
// t, tn, csrfField et csrfToken sont rebranchées par requête (render).
func newPages(cat *catalog.Catalog) (*pages, error) {
	funcs := template.FuncMap{
		"t":  func(key string, _ ...any) string { return key },
		"tn": func(key string, _ int, _ ...any) string { return key },
	}
	for k, v := range websec.TemplateFuncsPlaceholder() {
		funcs[k] = v
	}
	tpl, err := template.New("").Funcs(funcs).ParseFS(web.Templates, "templates/layout/*.html")
	if err != nil {
		return nil, fmt.Errorf("gabarits : %w", err)
	}
	return &pages{tpl: tpl, cat: cat}, nil
}

// render exécute le gabarit name dans la langue lang (la négociation de langue
// arrive à la vague suivante : l'appelant passe pour l'instant DefaultLang).
// data reçoit .Lang ; la réponse est entièrement fabriquée avant d'être écrite.
func (p *pages) render(w http.ResponseWriter, r *http.Request, status int, lang, name string, data map[string]any) {
	clone, err := p.tpl.Clone()
	if err != nil {
		http.Error(w, "Erreur interne.", http.StatusInternalServerError)
		return
	}
	funcs := websec.TemplateFuncs(r)
	funcs["t"] = func(key string, kv ...any) string { return p.cat.T(lang, key, catalog.Pairs(kv...)) }
	funcs["tn"] = func(key string, n int, kv ...any) string { return p.cat.TN(lang, key, n, catalog.Pairs(kv...)) }
	clone.Funcs(funcs)
	if data == nil {
		data = map[string]any{}
	}
	data["Lang"] = lang
	var buf bytes.Buffer
	if err := clone.ExecuteTemplate(&buf, name, data); err != nil {
		http.Error(w, "Erreur interne.", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_, _ = w.Write(buf.Bytes())
}

// isLoopbackURL indique si l'hôte de l'adresse est localhost, 127.0.0.1 ou ::1.
func isLoopbackURL(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil {
		return false
	}
	switch strings.ToLower(u.Hostname()) {
	case "localhost", "127.0.0.1", "::1":
		return true
	}
	return false
}

// canonicalNav renvoie les 5 entrées canoniques de l'administration ; active
// est la clé marquée active.
func canonicalNav(active string) []adminnav.Item {
	r := adminnav.New()
	for _, e := range []adminnav.Entry{
		{Key: "dashboard", Label: "layout.nav.dashboard", Icon: "dashboard", Rank: adminnav.RankDashboard, Destination: "/_skeleton/admin"},
		{Key: "participants", Label: "layout.nav.participants", Icon: "participants", Rank: adminnav.RankParticipants, Destination: "/_skeleton/admin"},
		{Key: "draw", Label: "layout.nav.draw", Icon: "draw", Rank: adminnav.RankDraw, Destination: "/_skeleton/admin"},
		{Key: "registrations", Label: "layout.nav.registrations", Icon: "registrations", Rank: adminnav.RankRegistrations, Destination: "/_skeleton/admin"},
		{Key: "more", Label: "layout.nav.more", Icon: "more", Rank: adminnav.RankMore, Destination: "/_skeleton/admin"},
	} {
		r.Declare(e)
	}
	return r.Items(active)
}

// staticHandler sert /static/ depuis web.Static avec un cache modéré : les
// fichiers ne sont pas empreintés, donc revalidation obligatoire.
func staticHandler() (http.Handler, error) {
	sub, err := fs.Sub(web.Static, "static")
	if err != nil {
		return nil, err
	}
	fsrv := http.StripPrefix("/static/", http.FileServerFS(sub))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "public, max-age=300, must-revalidate")
		fsrv.ServeHTTP(w, r)
	}), nil
}

// registerWeb branche /static/, la page d'accueil et, en bouclage seulement,
// la page squelette d'administration.
func registerWeb(a *App) error {
	pg, err := newPages(a.Texts.Cat)
	if err != nil {
		return err
	}
	st, err := staticHandler()
	if err != nil {
		return err
	}
	a.Mux.Handle("GET /static/", st)
	// Racine exacte seulement : le reste répond 404 par défaut du mux.
	a.Mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
		pg.render(w, r, http.StatusOK, DefaultLang, "layout/home", nil)
	})
	// GABARIT DE TEST : la page squelette d'administration n'est accessible
	// sans connexion que si l'hôte de PUBLIC_URL est un bouclage ; sinon 404.
	// Elle ne doit pas exister en production.
	if isLoopbackURL(a.Cfg.PublicURL) {
		a.Mux.HandleFunc("GET /_skeleton/admin", func(w http.ResponseWriter, r *http.Request) {
			pg.render(w, r, http.StatusOK, DefaultLang, "layout/admin", map[string]any{
				"TitleKey": "layout.nav.dashboard",
				"Nav":      canonicalNav("dashboard"),
			})
		})
	}
	return nil
}
