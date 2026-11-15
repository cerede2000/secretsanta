package settingspages

import (
	"io/fs"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"

	"secretsanta/internal/websec"
	"secretsanta/web"
)

var (
	inlineStyleRe = regexp.MustCompile(`(?i)<style[\s>]|\sstyle\s*=`)
	inlineScript  = regexp.MustCompile(`(?i)<script(\s[^>]*)?>[^<]|\son[a-z]+\s*=`)
	dataURLRe     = regexp.MustCompile(`(?i)data:[a-z]+/|url\(\s*["']?data:`)
)

// F10c.AC8 : un GET sur une route modifiante renvoie 405 sans rien modifier.
func TestStateChangingRoutesRejectGet(t *testing.T) {
	e := newEnv(t)
	gid := e.group("methodes")
	ids := e.addN(gid, "Hortensia", 3)
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
			req := httptest.NewRequest(m, "/methodes/admin/x", nil)
			req.SetPathValue("slug", "methodes")
			rec := httptest.NewRecorder()
			r.Handler.ServeHTTP(rec, req)
			if rec.Code != http.StatusMethodNotAllowed {
				t.Errorf("%s %s : 405 attendu, %d", m, r.Pattern, rec.Code)
			}
		}
	}
	if n != 4 {
		t.Fatalf("routes modifiantes : %d", n)
	}
	ck := e.adminCookie(gid)
	for _, p := range []string{"/methodes/admin/reglages/groupe?name=Pirate&budget=99", "/methodes/admin/reglages/relances?first=2",
		"/methodes/admin/reglages/courrier/test?test_to=a@b.fr"} {
		if rec := e.get(p, "fr", ck); rec.Code != http.StatusMethodNotAllowed && rec.Code != http.StatusNotFound {
			t.Errorf("GET %s par la chaîne : %d", p, rec.Code)
		}
	}
	e.get("/methodes/admin/reglages", "fr", ck)
	e.get("/methodes/admin/relances?type=lien&person="+itoa(ids[0]), "fr", ck)
	if e.row(gid).name != "Groupe methodes" || e.mail.calls != 0 || e.rem.calls != 0 || e.reveal.setCalls != 0 ||
		countRows(t, e.db, `SELECT COUNT(*) FROM reminder_group_rules`) != 0 {
		t.Fatal("un GET a modifié l'état")
	}
}

func TestPostWithoutCSRFTokenOrSessionIsRefused(t *testing.T) {
	e := newEnv(t)
	gid := e.group("csrf")
	ck := e.adminCookie(gid)
	for _, p := range []string{"reglages/groupe", "reglages/relances", "reglages/courrier/test", "relances"} {
		req := httptest.NewRequest(http.MethodPost, "/csrf/admin/"+p, strings.NewReader(url.Values{"name": {"Pirate"}}.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.Header.Set("Sec-Fetch-Site", "same-origin")
		req.AddCookie(ck)
		rec := httptest.NewRecorder()
		e.handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusForbidden {
			t.Errorf("POST %s sans jeton : %d", p, rec.Code)
		}
	}
	if e.row(gid).name != "Groupe csrf" {
		t.Fatal("rien ne devait être écrit")
	}
	other := e.group("csrf2")
	// Avec la session d'un autre groupe : refus.
	rec := e.post("/csrf/admin/reglages/groupe", "/csrf2/admin/reglages", e.adminCookie(other), groupFields("Pirate"))
	if rec.Code == http.StatusSeeOther || e.row(gid).name != "Groupe csrf" {
		t.Fatalf("session d'un autre groupe : %d", rec.Code)
	}
}

func TestRhythmAndManualReminderPosts(t *testing.T) {
	e := newEnv(t)
	gid := e.group("rythme")
	ids := e.addN(gid, "Hortensia", 3)
	ck := e.adminCookie(gid)
	page := "/rythme/admin/reglages"
	if rec := e.post(page+"/relances", page, ck, url.Values{"enabled": {"1"}, "first": {"5"}, "spacing": {"6"}, "cap": {"2"}}); rec.Code != 303 {
		t.Fatalf("rythme : %d", rec.Code)
	}
	st, _ := e.pages.o.Rhythm.Settings(ctx, gid)
	if !st.Enabled || st.FirstDelayDays != 5 || st.SpacingDays != 6 || st.MaxPerType != 2 {
		t.Fatalf("rythme enregistré : %+v", st)
	}
	if rec := e.post(page+"/relances", page, ck, url.Values{"first": {"99"}, "spacing": {"6"}, "cap": {"2"}}); rec.Code != 422 {
		t.Errorf("rythme invalide : %d", rec.Code)
	}
	rp := "/rythme/admin/relances"
	if rec := e.post(rp, rp, ck, url.Values{"type": {"lettre"}}); rec.Code != 422 || e.rem.calls != 0 {
		t.Fatalf("sans personne : %d", rec.Code)
	}
	if rec := e.post(rp, rp, ck, url.Values{"type": {"lettre"}, "person": {itoa(ids[0]), itoa(ids[1])}}); rec.Code != 200 || e.rem.calls != 1 || len(e.rem.ids) != 2 {
		t.Fatalf("relance : %d %+v", rec.Code, e.rem)
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
	for _, dir := range []string{"templates/admin/settings", "static/css/admin/settings"} {
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
	if count < 3 {
		t.Fatalf("fichiers vérifiés : %d", count)
	}
}

func TestRenderedPagesHaveNoInlineStyleOrDataURL(t *testing.T) {
	e := newEnv(t)
	gid := e.group("rendu")
	e.addN(gid, "Hortensia", 3)
	ck := e.adminCookie(gid)
	for _, p := range []string{"/rendu/admin/reglages", "/rendu/admin/relances"} {
		rec := e.get(p, "fr", ck)
		if rec.Code != 200 {
			t.Fatalf("%s : %d", p, rec.Code)
		}
		body := rec.Body.String()
		if inlineStyleRe.MatchString(body) || dataURLRe.MatchString(body) || strings.Contains(body, "<script>") {
			t.Errorf("%s : contenu interdit par la CSP", p)
		}
	}
}
