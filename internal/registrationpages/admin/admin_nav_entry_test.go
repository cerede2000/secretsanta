package admin

import (
	"net/http"
	"strings"
	"testing"

	"secretsanta/internal/shared/adminnav"
)

// F11c.AC6 : l'entrée « Inscriptions » se déclare seule, sans F10.
func TestNavEntryDeclaredAlone(t *testing.T) {
	r := adminnav.New()
	DeclareNav(r)
	items := r.Items(NavKey)
	if len(items) != 1 {
		t.Fatalf("%d entrées, 1 attendue", len(items))
	}
	it := items[0]
	if it.Key != "inscriptions" || it.Label != "layout.nav.registrations" || it.Rank != adminnav.RankRegistrations || !it.Active {
		t.Errorf("entrée inattendue : %+v", it)
	}
	if it.Destination != RegistrationPath {
		t.Errorf("destination %q", it.Destination)
	}
}

// Elle se place entre Tirage et Plus, quel que soit l'ordre de déclaration.
func TestNavEntryRank(t *testing.T) {
	r := adminnav.New()
	r.Declare(adminnav.Entry{Key: "plus", Rank: adminnav.RankMore})
	DeclareNav(r)
	r.Declare(adminnav.Entry{Key: "tirage", Rank: adminnav.RankDraw})
	var keys []string
	for _, it := range r.Items("") {
		keys = append(keys, it.Key)
	}
	if strings.Join(keys, ",") != "tirage,inscriptions,plus" {
		t.Errorf("ordre %v", keys)
	}
}

// La page s'affiche dans le gabarit d'administration de F1, entrée active.
func TestPageRendersInAdminTemplate(t *testing.T) {
	e := newEnv(t)
	w := e.do("GET", "/famille/admin/inscriptions", "fr", nil, e.cookies["famille"])
	if w.Code != http.StatusOK {
		t.Fatalf("statut %d", w.Code)
	}
	b := w.Body.String()
	for _, want := range []string{`<body class="admin">`, `class="admin-nav"`, `href="/famille/admin/inscriptions" aria-current="page"`, "Inscriptions par lien"} {
		if !strings.Contains(b, want) {
			t.Errorf("%q absent", want)
		}
	}
	if strings.Contains(b, "{slug}") {
		t.Error("{slug} non résolu")
	}
}

func TestPageNeedsAdminSessionOfTheGroup(t *testing.T) {
	e := newEnv(t)
	if w := e.do("GET", "/famille/admin/inscriptions", "", nil, nil); w.Code != 303 || w.Header().Get("Location") != "/famille/admin/connexion" {
		t.Errorf("sans session : %d %q", w.Code, w.Header().Get("Location"))
	}
	// La session d'un autre groupe ne vaut rien ici.
	if w := e.do("GET", "/famille/admin/inscriptions", "", nil, e.cookies["amis"]); w.Code != 303 {
		t.Errorf("autre groupe : %d", w.Code)
	}
	if w := e.do("GET", "/inconnu/admin/inscriptions", "", nil, e.cookies["famille"]); w.Code != 404 {
		t.Errorf("groupe inconnu : %d", w.Code)
	}
	// Les inscriptions d'un autre groupe n'apparaissent pas.
	b := e.do("GET", "/famille/admin/inscriptions", "", nil, e.cookies["famille"]).Body.String()
	if strings.Contains(b, "autre@exemple.fr") || !strings.Contains(b, "jeanne@exemple.fr") {
		t.Error("liste d'attente du mauvais groupe")
	}
}
