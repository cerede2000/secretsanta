package maildiagpages_test

import (
	"net/http"
	"net/url"
	"strings"
	"testing"
)

// F6c.AC11 : session administrateur du groupe, jeton CSRF, isolation des groupes.
func TestPagesRequireAdminSessionOfTheGroup(t *testing.T) {
	e := newEnv(t)
	if w := e.request("GET", pagePath, "", "", nil, nil); w.Code != http.StatusSeeOther || w.Header().Get("Location") != "/famille/admin/connexion" {
		t.Errorf("sans session : %d %q", w.Code, w.Header().Get("Location"))
	}
	// La session de « famille » ne vaut rien pour « amis ».
	if w := e.get("/amis/admin/courrier/delivrabilite", ""); w.Code != http.StatusSeeOther || w.Header().Get("Location") != "/amis/admin/connexion" {
		t.Errorf("autre groupe : %d %q", w.Code, w.Header().Get("Location"))
	}
	if w := e.get("/inconnu/admin/courrier/delivrabilite", ""); w.Code != http.StatusNotFound {
		t.Errorf("groupe inconnu : %d", w.Code)
	}
	tok, _ := e.csrf(t)
	for _, p := range []string{"/amis/admin/courrier/delivrabilite/test", "/amis/admin/courrier/delivrabilite/suppression/retirer"} {
		form := url.Values{"to": {"a@exemple.fr"}, "address": {"x@exemple.fr"}, "csrf_token": {tok}}
		if w := e.request("POST", p, "", "application/x-www-form-urlencoded", strings.NewReader(form.Encode()), e.cookies); w.Code == http.StatusOK || w.Code == http.StatusSeeOther && !strings.HasSuffix(w.Header().Get("Location"), "/connexion") {
			t.Errorf("POST %s avec la session d'un autre groupe : %d", p, w.Code)
		}
	}
	if len(e.mailer.items) != 0 {
		t.Error("un courriel a été mis en file pour un autre groupe")
	}
}

func TestPostsNeedCSRFToken(t *testing.T) {
	e := newEnv(t)
	if err := e.supp.Add(t.Context(), e.gid, "rebond@exemple.fr", "550 inconnu"); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{testPath, pagePath + "/suppression/retirer"} {
		form := url.Values{"to": {"a@exemple.fr"}, "address": {"rebond@exemple.fr"}}
		w := e.request("POST", p, "", "application/x-www-form-urlencoded", strings.NewReader(form.Encode()), e.cookies)
		if w.Code != http.StatusForbidden {
			t.Errorf("POST %s sans jeton : %d", p, w.Code)
		}
	}
	if ok, _ := e.supp.IsSuppressed(t.Context(), e.gid, "rebond@exemple.fr"); !ok || len(e.mailer.items) != 0 {
		t.Error("un POST sans jeton a modifié l'état")
	}
}

func TestSuppressionListIsPerGroupAndRemovable(t *testing.T) {
	e := newEnv(t)
	ctx := t.Context()
	_ = e.supp.Add(ctx, e.gid, "ici@exemple.fr", "550 boîte inconnue")
	_ = e.supp.Add(ctx, e.gid2, "ailleurs@exemple.fr", "550 secret")
	body := e.get(pagePath, "").Body.String()
	if !strings.Contains(body, "ici@exemple.fr") || !strings.Contains(body, "550 boîte inconnue") {
		t.Error("adresse du groupe absente")
	}
	if strings.Contains(body, "ailleurs@exemple.fr") || strings.Contains(body, "550 secret") {
		t.Error("adresse d'un autre groupe visible")
	}
	// Retirer l'adresse d'un autre groupe est sans effet sur ce groupe-là.
	e.post(t, pagePath+"/suppression/retirer", "", url.Values{"address": {"ailleurs@exemple.fr"}})
	if ok, _ := e.supp.IsSuppressed(ctx, e.gid2, "ailleurs@exemple.fr"); !ok {
		t.Error("l'adresse d'un autre groupe a été retirée")
	}
	w := e.post(t, pagePath+"/suppression/retirer", "", url.Values{"address": {"ici@exemple.fr"}})
	if w.Code != http.StatusSeeOther {
		t.Fatalf("retrait : %d", w.Code)
	}
	if ok, _ := e.supp.IsSuppressed(ctx, e.gid, "ici@exemple.fr"); ok {
		t.Error("adresse non retirée")
	}
	if page := e.get(w.Header().Get("Location"), "").Body.String(); !strings.Contains(page, `role="status"`) || strings.Contains(page, "ici@exemple.fr") {
		t.Error("page après retrait")
	}
}
