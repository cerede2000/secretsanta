package invite

import (
	"html"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"secretsanta/internal/websec"
)

// F11.AC9 : requêtes modifiantes sans jeton CSRF valide rejetées ; toutes les
// pages d'inscription portent Referrer-Policy no-referrer ; aucune route GET
// ne modifie l'état.

func TestPostWithoutValidCSRFIsRejected(t *testing.T) {
	e := newEnv(t)
	l := e.enable(t, e.g1)
	h := e.server(t)
	page := get(h, l.Path)
	m := csrfRe.FindStringSubmatch(page.Body.String())
	if m == nil {
		t.Fatal("formulaire sans jeton CSRF")
	}
	tok := html.UnescapeString(m[1])
	cookies := page.Result().Cookies()
	form := func(csrf string) url.Values {
		f := formOf("Anne", "Lebrun", "anne@example.org")
		if csrf != "" {
			f.Set("csrf_token", csrf)
		}
		return f
	}
	cases := []struct {
		name       string
		f          url.Values
		cookies    []*http.Cookie
		sameOrigin bool
	}{
		{"sans jeton", form(""), cookies, true},
		{"jeton faux", form("AAAA" + tok[4:]), cookies, true},
		{"sans cookie", form(tok), nil, true},
		{"origine étrangère", form(tok), cookies, false},
	}
	for _, c := range cases {
		rec := rawPost(h, l.Path, c.f, c.cookies, c.sameOrigin)
		if rec.Code != http.StatusForbidden {
			t.Errorf("%s : %d", c.name, rec.Code)
		}
	}
	if e.pendingCount(t, e.g1) != 0 || e.codes.count() != 0 {
		t.Fatal("inscription créée sans CSRF valide")
	}
	if rec := rawPost(h, l.Path, form(tok), cookies, true); rec.Code != http.StatusOK || e.pendingCount(t, e.g1) != 1 {
		t.Fatalf("POST valide : %d", rec.Code)
	}
}

func TestEveryRegistrationPageIsNoReferrer(t *testing.T) {
	e := newEnv(t)
	l := e.enable(t, e.g1)
	h := e.server(t)
	// h n'a pas SecurityHeaders : l'en-tête vient de l'inscription elle-même.
	check := func(name string, rec *httptest.ResponseRecorder) {
		t.Helper()
		if got := rec.Header().Get("Referrer-Policy"); got != "no-referrer" {
			t.Errorf("%s (%d) : Referrer-Policy %q", name, rec.Code, got)
		}
		if got := rec.Header().Get("Cache-Control"); got != "no-store" {
			t.Errorf("%s : Cache-Control %q", name, got)
		}
	}
	check("formulaire", get(h, l.Path))
	check("jeton faux", get(h, "/noel/rejoindre/faux"))
	check("slug inconnu", get(h, "/inconnu/rejoindre/"+l.Token))
	check("sans jeton", get(h, "/noel/rejoindre"))
	check("code envoyé", postForm(t, h, l.Path, formOf("Anne", "Lebrun", "anne@example.org")))
	check("champs invalides", postForm(t, h, l.Path, formOf("", "", "")))
	check("nom pris", postForm(t, h, l.Path, formOf("anne", "lebrun", "b@example.org")))
	check("adresse partagée", postForm(t, h, l.Path, formOf("Lou", "Lebrun", "anne@example.org")))
	// Le refus CSRF a lieu avant l'inscription : dans la chaîne de
	// l'application (SecurityHeaders, CSRF, …) il porte aussi no-referrer.
	pol, err := websec.NewPolicy(publicURL)
	if err != nil {
		t.Fatal(err)
	}
	app := websec.SecurityHeaders(pol)(h)
	refused := rawPost(app, l.Path, formOf("A", "B", "c@example.org"), nil, true)
	if refused.Code != http.StatusForbidden || refused.Header().Get("Referrer-Policy") != "no-referrer" {
		t.Errorf("refus CSRF : %d %q", refused.Code, refused.Header().Get("Referrer-Policy"))
	}
	check("formulaire (chaîne complète)", get(app, l.Path))

	open := get(h, l.Path)
	e.draw.Done[e.g1] = true
	check("inscriptions closes", get(h, l.Path))
	m := csrfRe.FindStringSubmatch(open.Body.String())
	f := formOf("Max", "Roux", "max@example.org")
	f.Set("csrf_token", html.UnescapeString(m[1]))
	check("POST clos", rawPost(h, l.Path, f, open.Result().Cookies(), true))
	if err := e.svc.Disable(t.Context(), admin(e.g1), e.g1); err != nil {
		t.Fatal(err)
	}
	check("mode désactivé", get(h, l.Path))
}

func TestGetNeverChangesState(t *testing.T) {
	e := newEnv(t)
	l := e.enable(t, e.g1)
	h := e.server(t)
	q := url.Values{FieldFirstName: {"Anne"}, FieldName: {"Lebrun"}, FieldEmail: {"anne@example.org"}, FieldConfirmShared: {"1"}}
	for _, m := range []string{http.MethodGet, http.MethodHead} {
		req := httptest.NewRequest(m, l.Path+"?"+q.Encode(), strings.NewReader(q.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.Header.Set("Sec-Fetch-Site", "same-origin")
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("%s : %d", m, rec.Code)
		}
	}
	if e.pendingCount(t, e.g1) != 0 || e.codes.count() != 0 {
		t.Fatal("un GET a créé une inscription")
	}
	inv, _ := e.store.Invitation(t.Context(), e.g1)
	if !inv.Enabled {
		t.Fatal("un GET a modifié l'invitation")
	}

	// Le gestionnaire lui-même (hors middleware CSRF) n'accepte que GET,
	// HEAD et POST ; la soumission refuse toute méthode sûre.
	form, err := (&Handlers{Service: e.svc, Render: pageRenderer{}}).InviteForm(t.Context(), "noel", l.Token)
	if err != nil || form == nil {
		t.Fatalf("formulaire : %v", err)
	}
	for _, m := range []string{http.MethodPut, http.MethodDelete, http.MethodPatch, http.MethodOptions} {
		rec := httptest.NewRecorder()
		form.ServeHTTP(rec, httptest.NewRequest(m, l.Path, strings.NewReader(q.Encode())))
		if rec.Code != http.StatusMethodNotAllowed {
			t.Errorf("%s : %d", m, rec.Code)
		}
	}
	if e.pendingCount(t, e.g1) != 0 {
		t.Fatal("une méthode non prévue a créé une inscription")
	}
}
