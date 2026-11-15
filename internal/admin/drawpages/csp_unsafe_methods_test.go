package drawpages

import (
	"io/fs"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"
	"time"

	"secretsanta/internal/participants"
	"secretsanta/internal/websec"
	"secretsanta/web"
)

var (
	inlineStyleRe = regexp.MustCompile(`(?i)<style[\s>]|\sstyle\s*=`)
	inlineScript  = regexp.MustCompile(`(?i)<script(\s[^>]*)?>[^<]|\son[a-z]+\s*=`)
	dataURLRe     = regexp.MustCompile(`(?i)data:[a-z]+/|url\(\s*["']?data:`)
)

// F10.AC38 : un GET sur une route modifiante renvoie 405 sans rien modifier.
func TestStateChangingRoutesRejectGet(t *testing.T) {
	e := newEnv(t)
	gid := e.group("methodes")
	e.addN(gid, "Hortensia", 4, participants.RoleNormal)
	n := 0
	for _, r := range e.pages.Routes() {
		if strings.HasPrefix(r.Pattern, "POST ") != r.Modifies {
			t.Errorf("%s : Modifies=%v incohérent", r.Pattern, r.Modifies)
		}
		if !r.Modifies {
			continue
		}
		n++
		for _, m := range []string{http.MethodGet, http.MethodHead, http.MethodOptions} {
			req := httptest.NewRequest(m, "/methodes/admin/tirage/x", nil)
			req.SetPathValue("slug", "methodes")
			rec := httptest.NewRecorder()
			r.Handler.ServeHTTP(rec, req)
			if rec.Code != http.StatusMethodNotAllowed {
				t.Errorf("%s %s : 405 attendu, %d", m, r.Pattern, rec.Code)
			}
		}
	}
	if n != 3 {
		t.Fatalf("routes modifiantes : %d", n)
	}
	ck := e.adminCookie(gid)
	for _, p := range []string{"/methodes/admin/tirage/lancer", "/methodes/admin/tirage/date", "/methodes/admin/tirage/ouvrir"} {
		if rec := e.get(p, "fr", ck); rec.Code != http.StatusMethodNotAllowed && rec.Code != http.StatusNotFound {
			t.Errorf("GET %s par la chaîne : %d", p, rec.Code)
		}
	}
	for _, p := range []string{"/methodes/admin/tirage", "/methodes/admin/tirage?confirmer=lancer", "/methodes/admin/tirage?confirmer=ouvrir",
		"/methodes/admin/tirage?onglet=historique", "/methodes/admin/tirage?onglet=ajuster"} {
		e.get(p, "fr", ck)
	}
	if e.drawCount(gid) != 0 || e.reveal.setCalls != 0 || e.reveal.openCall != 0 ||
		countRows(t, e.db, `SELECT COUNT(*) FROM admin_audit`) != 0 {
		t.Fatal("un GET a modifié l'état")
	}
}

func TestPostWithoutCSRFTokenIsRefused(t *testing.T) {
	e := newEnv(t)
	gid := e.group("csrf")
	e.addN(gid, "Hortensia", 4, participants.RoleNormal)
	for _, p := range []string{"lancer", "date", "ouvrir"} {
		req := httptest.NewRequest(http.MethodPost, "/csrf/admin/tirage/"+p, strings.NewReader(url.Values{"confirm": {"1"}}.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.Header.Set("Sec-Fetch-Site", "same-origin")
		req.AddCookie(e.adminCookie(gid))
		rec := httptest.NewRecorder()
		e.handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusForbidden {
			t.Errorf("POST %s sans jeton : %d", p, rec.Code)
		}
	}
	if e.drawCount(gid) != 0 || e.reveal.openCall != 0 {
		t.Fatal("rien ne devait être écrit")
	}
	// Sans session administrateur : refus aussi, même avec un jeton valide.
	ck := e.adminCookie(gid)
	other := e.group("csrf2")
	rec := e.post("/csrf/admin/tirage/lancer", "/csrf/admin/tirage?confirmer=lancer", ck, url.Values{"confirm": {"1"}})
	if rec.Code != http.StatusSeeOther || e.drawCount(gid) != 1 {
		t.Fatalf("POST valide : %d", rec.Code)
	}
	if countRows(t, e.db, `SELECT COUNT(*) FROM draws WHERE group_id = ?`, other) != 0 {
		t.Fatal("aucune action ne traverse un autre groupe")
	}
}

func TestRevealDateAndOpenNow(t *testing.T) {
	e := newEnv(t)
	gid := e.group("revelation")
	e.addN(gid, "Hortensia", 4, participants.RoleNormal)
	e.doDraw(gid)
	e.reveal.targets = 4
	ck := e.adminCookie(gid)
	page := "/revelation/admin/tirage"

	rec := e.post(page+"/date", page, ck, url.Values{"date": {"2026-12-01T18:00"}})
	if rec.Code != http.StatusSeeOther || e.reveal.at == nil || !e.reveal.at.Equal(time.Date(2026, 12, 1, 18, 0, 0, 0, time.UTC)) {
		t.Fatalf("date : %d %v", rec.Code, e.reveal.at)
	}
	if rec := e.get(page, "fr", ck); !strings.Contains(rec.Body.String(), `value="2026-12-01T18:00"`) {
		t.Error("la date enregistrée doit être préremplie")
	}
	if rec := e.post(page+"/date", page, ck, url.Values{"date": {"pas une date"}}); rec.Code != http.StatusUnprocessableEntity {
		t.Errorf("date invalide : %d", rec.Code)
	}
	// Sans confirmation : refus ; avec : ouverture, consignée.
	if rec := e.post(page+"/ouvrir", page, ck, nil); rec.Code != http.StatusUnprocessableEntity || e.reveal.openCall != 0 {
		t.Fatalf("ouvrir sans confirmation : %d", rec.Code)
	}
	if body := e.get(page+"?confirmer=ouvrir", "fr", ck).Body.String(); !strings.Contains(body, "4 participants") {
		t.Error("le volet de confirmation doit donner le nombre de participants")
	}
	if rec := e.post(page+"/ouvrir", page, ck, url.Values{"confirm": {"1"}}); rec.Code != http.StatusSeeOther || e.reveal.openCall != 1 {
		t.Fatalf("ouvrir : %d", rec.Code)
	}
	if countRows(t, e.db, `SELECT COUNT(*) FROM admin_audit WHERE action IN ('date_revelation','revelation_ouverte')`) != 2 {
		t.Error("date et ouverture doivent être consignées")
	}
	// Ouverte : la date n'est plus modifiable et « ouvrir maintenant » disparaît.
	body := e.get(page, "fr", ck).Body.String()
	if strings.Contains(body, `name="date"`) || strings.Contains(body, "confirmer=ouvrir") {
		t.Error("date et ouverture ne doivent plus être proposées une fois la révélation ouverte")
	}
}

func TestTemplatesAndAssetsHaveNoInlineStyleScriptOrDataURL(t *testing.T) {
	check := func(name, src string) {
		if inlineStyleRe.MatchString(src) {
			t.Errorf("%s : style en ligne", name)
		}
		if inlineScript.MatchString(src) {
			t.Errorf("%s : script ou gestionnaire en ligne", name)
		}
		if dataURLRe.MatchString(src) {
			t.Errorf("%s : ressource data:", name)
		}
		for _, p := range websec.CheckTemplate(name, src) {
			t.Errorf("%s : %s", name, p)
		}
	}
	count := 0
	for _, dir := range []string{"templates/admin/draw", "static/css/admin/draw"} {
		var fsys fs.FS = web.Templates
		if strings.HasPrefix(dir, "static") {
			fsys = web.Static
		}
		entries, err := fs.ReadDir(fsys, dir)
		if err != nil {
			t.Fatal(err)
		}
		for _, en := range entries {
			b, err := fs.ReadFile(fsys, dir+"/"+en.Name())
			if err != nil {
				t.Fatal(err)
			}
			check(dir+"/"+en.Name(), string(b))
			count++
		}
	}
	if count < 2 {
		t.Fatalf("fichiers vérifiés : %d", count)
	}
}

func TestRenderedPagesHaveNoInlineStyleOrDataURL(t *testing.T) {
	e := newEnv(t)
	gid := e.group("rendu")
	e.addN(gid, "Hortensia", 4, participants.RoleNormal)
	e.doDraw(gid)
	ck := e.adminCookie(gid)
	for _, p := range []string{"/rendu/admin/tirage", "/rendu/admin/tirage?confirmer=lancer", "/rendu/admin/tirage?confirmer=ouvrir",
		"/rendu/admin/tirage?onglet=ajuster", "/rendu/admin/tirage?onglet=historique"} {
		rec := e.get(p, "fr", ck)
		if rec.Code != 200 {
			t.Fatalf("%s : %d", p, rec.Code)
		}
		body := rec.Body.String()
		if inlineStyleRe.MatchString(body) || dataURLRe.MatchString(body) || strings.Contains(body, "<script>") || strings.Contains(body, "alert(") {
			t.Errorf("%s : contenu interdit par la CSP", p)
		}
	}
}
