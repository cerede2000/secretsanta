package magic_test

import (
	"context"
	"crypto/subtle"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sync"
	"testing"

	"secretsanta/internal/access/magic"
)

// groups simule F2 (pages publiques) et F11 (mode d'inscription et jeton).
type groups struct {
	mu      sync.Mutex
	names   map[string]string // slug -> nom
	active  map[string]bool   // mode d'inscription
	tokens  map[string]string // slug -> jeton en vigueur
	failing map[string]bool   // le fournisseur échoue pour ce slug
}

func (g *groups) PublicPage(_ context.Context, slug string) (http.Handler, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.failing[slug] {
		return nil, errors.New("panne")
	}
	name, ok := g.names[slug]
	if !ok {
		return nil, nil
	}
	// La page publique ne lit pas le mode d'inscription.
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = io.WriteString(w, "<h1>"+name+"</h1><p>Bienvenue !</p>")
	}), nil
}

func (g *groups) InviteForm(_ context.Context, slug, token string) (http.Handler, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.failing[slug] {
		return nil, errors.New("panne")
	}
	cur, ok := g.tokens[slug]
	if _, exists := g.names[slug]; !exists || !ok || !g.active[slug] {
		return nil, nil
	}
	if subtle.ConstantTimeCompare([]byte(cur), []byte(token)) != 1 {
		return nil, nil
	}
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, "formulaire d'inscription")
	}), nil
}

func (g *groups) set(f func()) {
	g.mu.Lock()
	defer g.mu.Unlock()
	f()
}

type snapshot struct {
	Status int
	Header http.Header
	Body   string
}

func get(t *testing.T, h http.Handler, path string) snapshot {
	t.Helper()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	res := rec.Result()
	b, _ := io.ReadAll(res.Body)
	return snapshot{Status: res.StatusCode, Header: res.Header, Body: string(b)}
}

func newInviteMux(g *groups) *http.ServeMux {
	p := &magic.InvitationPaths{Pages: g, Forms: g, Neutral: magic.NewNeutral(nil)}
	mux := http.NewServeMux()
	mux.Handle(magic.PatternPublic, p.Public())
	mux.Handle(magic.PatternJoin, p.Join())
	mux.Handle(magic.PatternJoinBare, p.Join())
	return mux
}

const validToken = "q3Vb0f9xZ2kLmN8pR1sT4w"

func fixtureGroups() *groups {
	return &groups{
		names:   map[string]string{"noel": "Noël des Martin", "ferme": "Noël fermé", "panne": "Groupe en panne"},
		active:  map[string]bool{"noel": true, "ferme": false},
		tokens:  map[string]string{"noel": validToken, "ferme": validToken, "panne": validToken},
		failing: map[string]bool{"panne": true},
	}
}

// F2b.AC6 : même réponse neutre (statut, corps, en-têtes) pour un slug
// inconnu, un jeton absent, invalide ou renouvelé et un mode désactivé, sur
// /<slug>/rejoindre/<jeton> comme sur /<slug>.
func TestInvitationPathNeutralResponseIsUniform(t *testing.T) {
	g := fixtureGroups()
	mux := newInviteMux(g)

	cases := map[string]string{
		"page d'un slug inconnu":              "/inconnu",
		"rejoindre : slug inconnu":            "/inconnu/rejoindre/" + validToken,
		"rejoindre : slug inconnu sans jeton": "/inconnu/rejoindre/",
		"rejoindre : jeton absent (barre)":    "/noel/rejoindre/",
		"rejoindre : jeton absent (nu)":       "/noel/rejoindre",
		"rejoindre : jeton invalide":          "/noel/rejoindre/mauvais-jeton",
		"rejoindre : jeton presque bon":       "/noel/rejoindre/" + validToken[:21] + "x",
		"rejoindre : jeton avec barre":        "/noel/rejoindre/" + validToken + "/x",
		"rejoindre : mode désactivé":          "/ferme/rejoindre/" + validToken,
		"rejoindre : mode désactivé, faux":    "/ferme/rejoindre/mauvais-jeton",
		"rejoindre : fournisseur en panne":    "/panne/rejoindre/" + validToken,
		"page : fournisseur en panne":         "/panne",
	}
	ref := get(t, mux, "/inconnu")
	if ref.Status != http.StatusNotFound {
		t.Fatalf("la réponse neutre doit être 404, obtenu %d", ref.Status)
	}
	if ref.Body == "" || ref.Header.Get("Content-Type") == "" || ref.Header.Get("Cache-Control") != "no-store" {
		t.Fatalf("réponse neutre incomplète : %+v", ref)
	}
	check := func(label, path string) {
		t.Helper()
		got := get(t, mux, path)
		if !reflect.DeepEqual(got, ref) {
			t.Fatalf("%s (%s) : réponse différente de la réponse neutre\nobtenu  %+v\nattendu %+v", label, path, got, ref)
		}
	}
	for label, path := range cases {
		check(label, path)
	}

	// Jeton renouvelé : l'ancien jeton donne la réponse neutre.
	g.set(func() { g.tokens["noel"] = "NouveauJetonRenouvele0" })
	check("rejoindre : jeton renouvelé", "/noel/rejoindre/"+validToken)

	// Le bon jeton, mode actif : le formulaire (le test distingue bien).
	ok := get(t, mux, "/noel/rejoindre/NouveauJetonRenouvele0")
	if ok.Status != http.StatusOK || ok.Body != "formulaire d'inscription" {
		t.Fatalf("le bon jeton doit ouvrir le formulaire : %+v", ok)
	}

	// Mode désactivé ensuite : neutre à nouveau, à l'identique.
	g.set(func() { g.active["noel"] = false })
	check("rejoindre : mode désactivé après coup", "/noel/rejoindre/NouveauJetonRenouvele0")
}

// F2b.AC6 : la page publique est identique que le mode soit actif ou non.
func TestPublicPageIdenticalWhateverTheMode(t *testing.T) {
	g := fixtureGroups()
	mux := newInviteMux(g)
	on := get(t, mux, "/noel")
	if on.Status != http.StatusOK {
		t.Fatalf("page publique attendue, %+v", on)
	}
	g.set(func() { g.active["noel"] = false })
	off := get(t, mux, "/noel")
	g.set(func() { delete(g.tokens, "noel") })
	never := get(t, mux, "/noel")
	g.set(func() { g.active["noel"] = true; g.tokens["noel"] = "AutreJetonEnVigueur000" })
	renewed := get(t, mux, "/noel")
	for label, s := range map[string]snapshot{"désactivé": off, "jamais activé": never, "renouvelé": renewed} {
		if !reflect.DeepEqual(s, on) {
			t.Fatalf("page publique différente (%s) :\n%+v\n%+v", label, s, on)
		}
	}
}

// La réponse neutre par défaut reste la même entre deux instances (pas
// d'élément variable : date, identifiant…), et HEAD ne renvoie pas de corps.
func TestNeutralIsDeterministic(t *testing.T) {
	a, b := magic.NewNeutral(nil), magic.NewNeutral(nil)
	ra, rb := httptest.NewRecorder(), httptest.NewRecorder()
	a.ServeHTTP(ra, httptest.NewRequest(http.MethodGet, "/x", nil))
	b.ServeHTTP(rb, httptest.NewRequest(http.MethodGet, "/y/rejoindre/z", nil))
	if !reflect.DeepEqual(ra.Result().Header, rb.Result().Header) || ra.Body.String() != rb.Body.String() || ra.Code != rb.Code {
		t.Fatal("deux réponses neutres doivent être identiques")
	}
	rh := httptest.NewRecorder()
	a.ServeHTTP(rh, httptest.NewRequest(http.MethodHead, "/x", nil))
	if rh.Code != http.StatusNotFound || rh.Body.Len() != 0 {
		t.Fatalf("HEAD : 404 sans corps attendu, %d / %d octets", rh.Code, rh.Body.Len())
	}
}
