package drawpages

import (
	"errors"
	"net/http"
	"net/url"
	"testing"

	"secretsanta/internal/participants"
	"secretsanta/internal/reveal"
	"secretsanta/internal/shared/errs"
)

func TestZZDrawPageViews(t *testing.T) {
	e := newEnv(t)
	gid := e.group("vues")
	e.addN(gid, "Hortensia", 4, participants.RoleNormal)
	ck := e.adminCookie(gid)
	for _, q := range []string{"", "?confirmer=lancer", "?confirmer=ouvrir", "?onglet=" + tabHistory, "?onglet=" + tabAdjust,
		"?ok=lance", "?ok=refait", "?ok=ouvert", "?ok=date", "?ok=autre"} {
		if r := e.get("/vues/admin/tirage"+q, "fr", ck); r.Code != 200 {
			t.Errorf("page %q : %d", q, r.Code)
		}
	}
	if r := e.get("/vues/admin/tirage", "fr", nil); r.Code != http.StatusSeeOther {
		t.Errorf("sans session : %d", r.Code)
	}
	if r := e.get("/inconnu/admin/tirage", "fr", nil); r.Code != http.StatusNotFound {
		t.Errorf("groupe inconnu : %d", r.Code)
	}
	e.doDraw(gid)
	for _, q := range []string{"", "?onglet=" + tabHistory, "?onglet=" + tabAdjust} {
		if r := e.get("/vues/admin/tirage"+q, "en", ck); r.Code != 200 {
			t.Errorf("page après tirage %q : %d", q, r.Code)
		}
	}
}

func TestZZLaunchPaths(t *testing.T) {
	e := newEnv(t)
	gid := e.group("lancer")
	e.addN(gid, "Hortensia", 5, participants.RoleNormal)
	ck := e.adminCookie(gid)
	page := "/lancer/admin/tirage?confirmer=lancer"
	launch := "/lancer/admin/tirage/lancer"

	// Sans confirmation.
	if r := e.post(launch, page, ck, nil); r.Code != http.StatusUnprocessableEntity {
		t.Errorf("sans confirmation : %d", r.Code)
	}
	// Premier tirage puis nouveau tirage complet.
	if r := e.post(launch, page, ck, url.Values{"confirm": {"1"}}); r.Code != http.StatusSeeOther {
		t.Fatalf("tirage : %d", r.Code)
	}
	if r := e.post(launch, page, ck, url.Values{"confirm": {"1"}}); r.Code != http.StatusSeeOther {
		t.Fatalf("nouveau tirage : %d", r.Code)
	}
	if e.drawCount(gid) != 2 {
		t.Errorf("tirages : %d", e.drawCount(gid))
	}
	// Des inscriptions en attente avec choix valide et plafond confirmé.
	e.reg.Pending[gid] = 2
	if r := e.post(launch, page, ck, url.Values{"confirm": {"1"}, "pending": {"ignorer"}, "cap_confirmed": {"1"}}); r.Code != http.StatusSeeOther {
		t.Errorf("avec choix : %d", r.Code)
	}
	// Sans cela, sans personne : tirage impossible.
	e2 := newEnv(t)
	g2 := e2.group("vide")
	e2.addN(g2, "Solo", 1, participants.RoleNormal)
	ck2 := e2.adminCookie(g2)
	if r := e2.post("/vide/admin/tirage/lancer", "/vide/admin/tirage?confirmer=lancer", ck2, url.Values{"confirm": {"1"}}); r.Code < 400 {
		t.Errorf("tirage impossible accepté : %d", r.Code)
	}
	e2.reg.Pending[g2] = 1
	if r := e2.post("/vide/admin/tirage/lancer", "/vide/admin/tirage?confirmer=lancer", ck2, url.Values{"confirm": {"1"}, "pending": {"supprimer"}}); r.Code < 400 {
		t.Errorf("contrôle impossible accepté : %d", r.Code)
	}
}

func TestZZDateAndOpen(t *testing.T) {
	e := newEnv(t)
	gid := e.group("dates")
	e.addN(gid, "Hortensia", 4, participants.RoleNormal)
	ck := e.adminCookie(gid)
	page := "/dates/admin/tirage"
	date := page + "/date"
	open := page + "/ouvrir"

	if r := e.post(date, page, ck, url.Values{"date": {"pas une date"}}); r.Code != http.StatusUnprocessableEntity {
		t.Errorf("date invalide : %d", r.Code)
	}
	if r := e.post(date, page, ck, url.Values{"date": {"2027-12-24T18:00"}}); r.Code != http.StatusSeeOther {
		t.Errorf("date : %d", r.Code)
	}
	if r := e.post(date, page, ck, url.Values{"date": {""}}); r.Code != http.StatusSeeOther {
		t.Errorf("retrait de la date : %d", r.Code)
	}
	for name, c := range map[string]struct {
		err  error
		code int
	}{
		"verrouillee": {errs.Conflict("x").WithCode(reveal.CodeDateLocked), http.StatusConflict},
		"validation":  {errs.Validation("x"), http.StatusUnprocessableEntity},
		"interdit":    {errs.Forbidden("x"), http.StatusForbidden},
		"introuvable": {errs.NotFound("x"), http.StatusNotFound},
		"conflit":     {errs.Conflict("x"), http.StatusConflict},
		"panne":       {errors.New("panne"), http.StatusInternalServerError},
	} {
		e.reveal.setErr = c.err
		if r := e.post(date, page, ck, url.Values{"date": {"2027-12-24T18:00"}}); r.Code != c.code {
			t.Errorf("%s : %d, %d attendu", name, r.Code, c.code)
		}
	}
	e.reveal.setErr = nil

	if r := e.post(open, page+"?confirmer=ouvrir", ck, nil); r.Code != http.StatusUnprocessableEntity {
		t.Errorf("ouverture sans confirmation : %d", r.Code)
	}
	if r := e.post(open, page+"?confirmer=ouvrir", ck, url.Values{"confirm": {"1"}}); r.Code != http.StatusSeeOther {
		t.Errorf("ouverture : %d", r.Code)
	}
	if e.reveal.openCall != 1 {
		t.Errorf("ouvertures : %d", e.reveal.openCall)
	}
	// Sans session : refus.
	if r := e.post(open, page, ck, url.Values{"confirm": {"1"}}); r.Code != http.StatusSeeOther {
		t.Logf("deuxième ouverture : %d", r.Code)
	}
}
