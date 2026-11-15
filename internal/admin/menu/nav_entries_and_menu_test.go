package menu

import (
	"html"
	"strings"
	"testing"

	"secretsanta/internal/shared/adminnav"
)

// F10.AC8 : Tableau, Tirage et Plus sont déclarés (jamais Inscriptions : F11c).
func TestDeclareNav(t *testing.T) {
	r := adminnav.New()
	DeclareNav(r)
	got := map[string]adminnav.Item{}
	for _, it := range r.Items("") {
		got[it.Key] = it
	}
	if len(got) != 3 {
		t.Fatalf("%d entrées, 3 attendues", len(got))
	}
	for key, rank := range map[string]int{"tableau": adminnav.RankDashboard, "tirage": adminnav.RankDraw, "plus": adminnav.RankMore} {
		it, ok := got[key]
		if !ok || it.Rank != rank || it.Destination == "" || it.Icon == "" {
			t.Errorf("entrée %s : %+v", key, it)
		}
	}
	res := ResolveNav(r.Items("plus"), "famille")
	for _, it := range res {
		if strings.Contains(it.Destination, "{") || !strings.HasPrefix(it.Destination, "/famille/admin") {
			t.Errorf("destination non résolue : %q", it.Destination)
		}
	}
}

// La feuille contient les cinq entrées, la dernière séparée visuellement.
func TestMenuSheet(t *testing.T) {
	e := newEnv(t)
	fr := catalogFile(t, "fr")
	w := e.do("GET", "/famille/admin/plus", "fr", nil, e.cookies["famille"])
	if w.Code != 200 {
		t.Fatalf("statut %d", w.Code)
	}
	body := html.UnescapeString(w.Body.String())
	last := -1
	for _, k := range []string{"settings", "journal", "export", "help", "logout"} {
		i := strings.Index(body, fr["admin.menu."+k])
		if i < 0 {
			t.Fatalf("« %s » absent", fr["admin.menu."+k])
		}
		if i < last {
			t.Errorf("« %s » dans le désordre", k)
		}
		last = i
	}
	for _, want := range []string{"/famille/admin/reglages", "/famille/admin/journal", "/famille/admin/participants/export", "/famille/admin/aide"} {
		if !strings.Contains(body, `href="`+want+`"`) {
			t.Errorf("lien %s absent", want)
		}
	}
	// Déconnexion : formulaire POST à part, dans un groupe sensible distinct.
	if !strings.Contains(body, `class="admin-menu-group admin-menu-sensitive" method="post" action="/famille/admin/deconnexion"`) {
		t.Error("formulaire de déconnexion séparé absent")
	}
	if !strings.Contains(body, `name="csrf_token"`) {
		t.Error("jeton CSRF absent du formulaire de déconnexion")
	}
	// Barre de navigation : Plus est active, les entrées sont résolues.
	if !strings.Contains(body, `href="/famille/admin/plus" aria-current="page"`) {
		t.Error("entrée « Plus » non active")
	}
	if strings.Contains(body, "{slug}") {
		t.Error("destination non résolue dans le rendu")
	}
}

func TestMenuRequiresAdminOfThatGroup(t *testing.T) {
	e := newEnv(t)
	if w := e.do("GET", "/famille/admin/plus", "fr", nil, nil); w.Code != 303 || w.Header().Get("Location") != "/famille/admin/connexion" {
		t.Errorf("sans session : %d %q", w.Code, w.Header().Get("Location"))
	}
	if w := e.do("GET", "/famille/admin/plus", "fr", nil, e.cookies["amis"]); w.Code != 303 {
		t.Errorf("session d'un autre groupe : %d", w.Code)
	}
	if w := e.do("GET", "/inconnu/admin/plus", "fr", nil, e.cookies["famille"]); w.Code != 404 {
		t.Errorf("groupe inconnu : %d", w.Code)
	}
}
