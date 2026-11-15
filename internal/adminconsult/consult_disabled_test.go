package adminconsult

import (
	"net/http"
	"net/url"
	"strings"
	"testing"

	"secretsanta/internal/participants"
	"secretsanta/internal/shared/errs"
)

// F10b.AC1 : l'option est désactivée par défaut.
func TestConsultOptionIsOffByDefault(t *testing.T) {
	e := newEnv(t)
	gid := e.group("par-defaut", false)
	g, err := e.groups.ByID(ctx, gid)
	if err != nil {
		t.Fatal(err)
	}
	if g.AdminCanViewPair {
		t.Fatal("l'option de consultation doit être désactivée par défaut")
	}
	on, err := e.svc.Enabled(ctx, gid)
	if err != nil || on {
		t.Fatalf("Enabled = %v, %v ; attendu false", on, err)
	}
}

// F10b.AC1 : option désactivée, la consultation est refusée par le service,
// même avec le bon mot de passe, et rien n'est consigné.
func TestDisabledConsultationIsRefusedByService(t *testing.T) {
	e := newEnv(t)
	gid := e.group("ferme", false)
	ids := e.addN(gid, "Hortense", 4)
	e.doDraw(gid)
	admin := participants.AdminActor(gid)
	res, err := e.svc.Consult(ctx, admin, gid, ids[0], e.secrets[gid])
	if err == nil || res != nil {
		t.Fatalf("consultation acceptée alors que l'option est désactivée : %+v", res)
	}
	if code, _ := errs.CodeOf(err); code != CodeDisabled {
		t.Fatalf("code d'erreur %q, attendu %q", code, CodeDisabled)
	}
	if _, err := e.svc.People(ctx, admin, gid); err == nil {
		t.Fatal("la liste de choix est servie alors que l'option est désactivée")
	}
	if n := e.journalRows(gid); n != 0 {
		t.Fatalf("journal : %d ligne(s) pour une consultation refusée", n)
	}
}

// F10b.AC1 : option désactivée, la route n'est pas servie : GET et POST
// répondent exactement comme une route inconnue, pour l'administrateur
// connecté comme pour un visiteur, et rien n'est révélé ni consigné.
func TestDisabledRouteIsNotServed(t *testing.T) {
	e := newEnv(t)
	gid := e.group("sans-option", false)
	ids := e.addN(gid, "Hortense", 4)
	e.doDraw(gid)
	ck := e.adminCookie(gid)

	unknown := e.get("/sans-option/admin/route-inexistante", "fr", ck)
	if unknown.Code != http.StatusNotFound {
		t.Fatalf("référence : une route inconnue répond %d", unknown.Code)
	}
	for _, c := range []*http.Cookie{ck, nil} {
		rec := e.get("/sans-option/admin/consultation", "fr", c)
		if rec.Code != http.StatusNotFound || rec.Body.String() != unknown.Body.String() {
			t.Errorf("GET (cookie=%v) : %d %q, attendu la réponse d'une route inconnue", c != nil, rec.Code, rec.Body.String())
		}
		if loc := rec.Header().Get("Location"); loc != "" {
			t.Errorf("GET (cookie=%v) : redirection %q au lieu d'une route absente", c != nil, loc)
		}
	}
	rec := e.consult("sans-option", "fr", ck, url.Values{"person": {idStr(ids[0])}, "password": {e.secrets[gid]}})
	if rec.Code != http.StatusNotFound || rec.Body.String() != unknown.Body.String() {
		t.Fatalf("POST : %d %q, attendu la réponse d'une route inconnue", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	for _, id := range ids {
		for _, n := range e.names[id] {
			if strings.Contains(body, n) {
				t.Errorf("POST refusé : le nom %q apparaît", n)
			}
		}
	}
	if n := e.journalRows(gid); n != 0 {
		t.Fatalf("journal : %d ligne(s) alors que la route n'est pas servie", n)
	}

	// Témoin : la même route existe pour un groupe qui a l'option.
	on := e.group("avec-option", true)
	e.addN(on, "Ines", 3)
	if rec := e.get("/avec-option/admin/consultation", "fr", e.adminCookie(on)); rec.Code != http.StatusOK {
		t.Fatalf("témoin : la route devrait être servie quand l'option est active, %d", rec.Code)
	}
}
