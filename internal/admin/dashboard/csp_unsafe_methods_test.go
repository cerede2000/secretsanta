package dashboard

import (
	"io/fs"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"

	"secretsanta/internal/participants"
	"secretsanta/internal/websec"
	"secretsanta/web"
)

var (
	inlineStyleRe = regexp.MustCompile(`(?i)<style[\s>]|\sstyle\s*=`)
	inlineScript  = regexp.MustCompile(`(?i)<script(\s[^>]*)?>[^<]|\son[a-z]+\s*=`)
	dataURLRe     = regexp.MustCompile(`(?i)data:[a-z]+/|url\(\s*["']?data:`)
)

// F10.AC32 : un GET sur une route modifiante renvoie 405 sans rien modifier.
func TestStateChangingRoutesRejectGet(t *testing.T) {
	e := newEnv(t)
	gid := e.group("methodes")
	pid := e.add(gid, "Mamie", "", participants.RoleNormal)
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
			req := httptest.NewRequest(m, "/methodes/admin/personnes/1/lien-remis", nil)
			req.SetPathValue("slug", "methodes")
			req.SetPathValue("id", "1")
			rec := httptest.NewRecorder()
			r.Handler.ServeHTTP(rec, req)
			if rec.Code != http.StatusMethodNotAllowed {
				t.Errorf("%s %s : 405 attendu, %d", m, r.Pattern, rec.Code)
			}
		}
	}
	if n < 1 {
		t.Fatal("aucune route modifiante")
	}
	// Par la chaîne complète, avec une vraie session : toujours 405 et rien d'écrit.
	ck := e.adminCookie(gid)
	rec := e.get("/methodes/admin/personnes/"+itoa(pid)+"/lien-remis", "fr", ck)
	if rec.Code != http.StatusMethodNotAllowed && rec.Code != http.StatusNotFound {
		t.Errorf("GET par la chaîne : %d", rec.Code)
	}
	if c := countRows(t, e.db, `SELECT COUNT(*) FROM admin_link_handover`) + countRows(t, e.db, `SELECT COUNT(*) FROM admin_audit`); c != 0 {
		t.Fatalf("un GET a modifié l'état : %d lignes", c)
	}
	// Les routes de lecture ne modifient rien non plus.
	for _, p := range []string{"/methodes/admin", "/methodes/admin/controle", "/methodes/admin/journal"} {
		e.get(p, "fr", ck)
	}
	if c := countRows(t, e.db, `SELECT COUNT(*) FROM admin_link_handover`) + countRows(t, e.db, `SELECT COUNT(*) FROM admin_audit`); c != 0 {
		t.Fatalf("une page de lecture a modifié l'état : %d lignes", c)
	}
}

func TestPostWithoutCSRFTokenIsRefused(t *testing.T) {
	e := newEnv(t)
	gid := e.group("csrf")
	pid := e.add(gid, "Mamie", "", participants.RoleNormal)
	req := httptest.NewRequest(http.MethodPost, "/csrf/admin/personnes/"+itoa(pid)+"/lien-remis", nil)
	req.Header.Set("Sec-Fetch-Site", "same-origin")
	req.AddCookie(e.adminCookie(gid))
	rec := httptest.NewRecorder()
	e.handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("POST sans jeton : %d", rec.Code)
	}
	if countRows(t, e.db, `SELECT COUNT(*) FROM admin_link_handover`) != 0 {
		t.Fatal("rien ne devait être écrit")
	}
	// Avec le jeton : la marque est posée, puis la page la montre.
	ck := e.adminCookie(gid)
	if rec := e.post("/csrf/admin/personnes/"+itoa(pid)+"/lien-remis", "/csrf/admin", ck); rec.Code != http.StatusSeeOther {
		t.Fatalf("POST avec jeton : %d", rec.Code)
	}
	if countRows(t, e.db, `SELECT COUNT(*) FROM admin_link_handover`) != 1 || countRows(t, e.db, `SELECT COUNT(*) FROM admin_audit`) != 1 {
		t.Fatal("la marque et le journal devaient être écrits")
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
	for _, dir := range []string{"templates/admin/dashboard", "static/css/admin/dashboard"} {
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
	if count < 4 {
		t.Fatalf("fichiers vérifiés : %d", count)
	}
}

func TestRenderedPagesHaveNoInlineStyleOrDataURL(t *testing.T) {
	e := newEnv(t)
	gid := e.group("rendu")
	e.addN(gid, "Hortensia", 3, participants.RoleNormal)
	e.add(gid, "Mamie", "", participants.RoleNormal)
	e.doDraw(gid)
	ck := e.adminCookie(gid)
	for _, p := range []string{"/rendu/admin", "/rendu/admin/controle", "/rendu/admin/journal"} {
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
