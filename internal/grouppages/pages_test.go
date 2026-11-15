package grouppages

import (
	"context"
	"html"
	"regexp"
	"strings"
	"testing"
)

var secretRe = regexp.MustCompile(`<code class="groups-secret" id="admin-secret">([^<]+)</code>`)

func TestCreateFlowShowsSecretOnceAndItWorks(t *testing.T) {
	e := newEnv(t)
	form := validForm("noel-2026")
	form.Set("budget", "25")
	form.Set("event_date", "2026-12-24")
	form.Set("reveal_at", "2026-12-01T18:00")
	form.Set("admin_can_view_pair", "1")
	w := e.postCreate(t, "fr", form)
	if w.Code != 200 {
		t.Fatalf("création : %d %s", w.Code, w.Body.String())
	}
	if w.Header().Get("Cache-Control") != "no-store" {
		t.Error("la page du secret doit être en no-store")
	}
	m := secretRe.FindStringSubmatch(w.Body.String())
	if m == nil {
		t.Fatalf("secret absent : %s", w.Body.String())
	}
	if _, err := e.svc.VerifyAdminSecret(context.Background(), "noel-2026", m[1]); err != nil {
		t.Errorf("le secret affiché ne fonctionne pas : %v", err)
	}
	body := w.Body.String()
	for _, want := range []string{`data-copy-target="admin-secret"`, `/static/js/groups/copy.js`, `http://127.0.0.1:8080/noel-2026`, `/noel-2026/admin`} {
		if !strings.Contains(body, want) {
			t.Errorf("page de création : %q absent", want)
		}
	}
	// Le secret n'apparaît ni sur l'accueil du groupe ni au rechargement du formulaire.
	for _, p := range []string{"/noel-2026", "/creer"} {
		if strings.Contains(e.do("GET", p, "", nil, nil).Body.String(), m[1]) {
			t.Errorf("%s : le secret réapparaît", p)
		}
	}
	g, err := e.svc.BySlug(context.Background(), "noel-2026")
	if err != nil {
		t.Fatal(err)
	}
	if g.BudgetCents == nil || *g.BudgetCents != 2500 || g.EventDate != "2026-12-24" || g.RevealAt == nil || !g.AdminCanViewPair {
		t.Errorf("groupe enregistré inattendu : %+v", g)
	}
}

func TestCreateBudgetOptionalAndFormKept(t *testing.T) {
	e := newEnv(t)
	if body := e.do("GET", "/creer", "", nil, nil).Body.String(); !strings.Contains(body, `name="budget"`) || strings.Contains(body, `name="budget" type="text" inputmode="decimal" autocomplete="off" placeholder="Par exemple 25" value="2`) {
		t.Error("le budget doit être présent et vide par défaut")
	}
	if w := e.postCreate(t, "", validForm("sans-budget")); w.Code != 200 {
		t.Fatalf("sans budget : %d", w.Code)
	}
	g, _ := e.svc.BySlug(context.Background(), "sans-budget")
	if g == nil || g.HasBudget() {
		t.Errorf("budget inattendu : %+v", g)
	}
	bad := validForm("garde-saisie")
	bad.Set("password", "faux")
	bad.Set("name", "Mon groupe")
	w := e.postCreate(t, "", bad)
	if w.Code != 401 {
		t.Errorf("mauvais mot de passe : %d", w.Code)
	}
	body := w.Body.String()
	if !strings.Contains(body, `role="alert"`) || !strings.Contains(body, `value="Mon groupe"`) || strings.Contains(body, "faux") {
		t.Errorf("erreur intégrée ou saisie conservée incorrecte : %s", body)
	}
}

func TestCreateErrorStatuses(t *testing.T) {
	e := newEnv(t)
	if w := e.postCreate(t, "", validForm("doublon")); w.Code != 200 {
		t.Fatal(w.Code)
	}
	if w := e.postCreate(t, "", validForm("doublon")); w.Code != 409 {
		t.Errorf("doublon : %d", w.Code)
	}
	if w := e.postCreate(t, "", validForm("admin")); w.Code != 422 {
		t.Errorf("slug réservé : %d", w.Code)
	}
	f := validForm("date-ko")
	f.Set("reveal_at", "pas une date")
	if w := e.postCreate(t, "", f); w.Code != 422 {
		t.Errorf("date de révélation : %d", w.Code)
	}
	for i := 0; i < 5; i++ {
		f := validForm("limite")
		f.Set("password", "faux")
		e.postCreate(t, "", f)
	}
	if w := e.postCreate(t, "", validForm("limite")); w.Code != 429 {
		t.Errorf("limiteur : %d", w.Code)
	}
}

func TestHomeShowsNameOnly(t *testing.T) {
	e := newEnv(t)
	e.postCreate(t, "", validForm("accueil"))
	w := e.do("GET", "/accueil", "", nil, nil)
	if w.Code != 200 || !strings.Contains(w.Body.String(), "Family Martin") {
		t.Fatalf("accueil : %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), `href="/accueil/connexion"`) {
		t.Error("lien d'entrée absent")
	}
	if w := e.do("GET", "/inconnu", "", nil, nil); w.Code != 404 {
		t.Errorf("inconnu : %d", w.Code)
	}
}

func TestNewRequiresDeps(t *testing.T) {
	if _, err := New(Deps{}); err == nil {
		t.Error("dépendances manquantes acceptées")
	}
}

// Les erreurs d'adresse s'affichent sous le champ (rouge, aria, role alert),
// avec la saisie dans le message ; la puce « Validé » prouve le mot de passe.
func TestCreateSlugFieldErrors(t *testing.T) {
	e := newEnv(t)
	e.postCreate(t, "", validForm("doublon"))
	for _, c := range []struct {
		slug, want string
		code       int
	}{
		{"admin", "« admin » est réservé à l'appli. Essaie famille-2026.", 422},
		{"Mauvaise Adresse!", "n'est pas une adresse valide", 422},
		{"doublon", "« doublon » est déjà pris", 409},
	} {
		w := e.postCreate(t, "", validForm(c.slug))
		body := html.UnescapeString(w.Body.String())
		if w.Code != c.code {
			t.Errorf("%s : statut %d", c.slug, w.Code)
		}
		for _, frag := range []string{c.want, `aria-invalid="true"`, `aria-describedby="slug-error"`, `id="slug-error" class="groups-alert groups-field-error" role="alert"`, "Validé"} {
			if !strings.Contains(body, frag) {
				t.Errorf("%s : %q absent", c.slug, frag)
			}
		}
		if strings.Contains(body, `id="create-error"`) {
			t.Errorf("%s : alerte générale en plus de l'erreur de champ", c.slug)
		}
	}
	// Mauvais mot de passe : aucune information sur l'adresse ni puce « Validé ».
	f := validForm("admin")
	f.Set("password", "faux")
	body := e.postCreate(t, "", f).Body.String()
	if strings.Contains(body, "slug-error") || strings.Contains(body, "Validé") || !strings.Contains(body, `id="create-error"`) {
		t.Errorf("mot de passe faux : %s", body)
	}
}
