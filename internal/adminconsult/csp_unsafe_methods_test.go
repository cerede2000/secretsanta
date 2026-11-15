package adminconsult

import (
	"io/fs"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"

	"secretsanta/web"
)

var (
	inlineStyleRe = regexp.MustCompile(`(?i)<style[\s>]|\sstyle\s*=`)
	inlineScript  = regexp.MustCompile(`(?i)<script(\s[^>]*)?>[^<]|\son[a-z]+\s*=`)
	dataURLRe     = regexp.MustCompile(`(?i)data:[a-z]+/|url\(\s*["']?data:`)
)

// F10b.AC13 : un GET (ou HEAD, OPTIONS) sur une route modifiante renvoie 405
// sans rien modifier ; les pages servies en GET ne consignent rien.
func TestStateChangingRoutesRejectGet(t *testing.T) {
	e := newEnv(t)
	gid := e.group("methodes", true)
	ids := e.addN(gid, "Hortense", 4)
	e.doDraw(gid)
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
			q := url.Values{"person": {idStr(ids[0])}, "password": {e.secrets[gid]}}
			req := httptest.NewRequest(m, "/methodes/admin/consultation?"+q.Encode(), nil)
			req.SetPathValue("slug", "methodes")
			req.AddCookie(e.adminCookie(gid))
			rec := httptest.NewRecorder()
			r.Handler.ServeHTTP(rec, req)
			if rec.Code != http.StatusMethodNotAllowed {
				t.Errorf("%s %s : 405 attendu, %d", m, r.Pattern, rec.Code)
			}
		}
	}
	if n != 1 {
		t.Fatalf("routes modifiantes : %d", n)
	}
	ck := e.adminCookie(gid)
	q := "?person=" + idStr(ids[0]) + "&password=" + url.QueryEscape(e.secrets[gid])
	for _, p := range []string{"/methodes/admin/consultation", "/methodes/admin/consultation" + q, "/methodes/consultations"} {
		rec := e.get(p, "fr", ck)
		if rec.Code != http.StatusOK {
			t.Errorf("GET %s : %d", p, rec.Code)
		}
		for _, n := range e.names[e.receiversOf(gid, ids[0])[0]] {
			if strings.Contains(rec.Body.String(), "<li class=\"adminconsult-target\">"+n) {
				t.Errorf("GET %s révèle une cible", p)
			}
		}
	}
	if rows := e.journalRows(gid); rows != 0 {
		t.Fatalf("un GET a consigné %d consultation(s)", rows)
	}
}

// Un POST sans jeton CSRF est refusé et ne consigne rien.
func TestPostWithoutCSRFTokenIsRefused(t *testing.T) {
	e := newEnv(t)
	gid := e.group("jeton-absent", true)
	ids := e.addN(gid, "Hortense", 4)
	e.doDraw(gid)
	body := url.Values{"person": {idStr(ids[0])}, "password": {e.secrets[gid]}}.Encode()
	rec := e.postWithToken("/jeton-absent/admin/consultation", "fr", "", e.adminCookie(gid), nil, body)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("POST sans jeton : %d", rec.Code)
	}
	if rows := e.journalRows(gid); rows != 0 {
		t.Fatalf("journal : %d", rows)
	}
}

// F10b.AC13 : aucun gabarit du module n'a de style en ligne, de script en
// ligne ni de ressource data:, et les pages rendues non plus.
func TestTemplatesAndPagesHaveNoInlineStyleNorDataURL(t *testing.T) {
	files, err := fs.Glob(web.Templates, "templates/adminconsult/*.html")
	if err != nil || len(files) == 0 {
		t.Fatalf("gabarits : %v %v", files, err)
	}
	for _, f := range files {
		b, err := fs.ReadFile(web.Templates, f)
		if err != nil {
			t.Fatal(err)
		}
		s := string(b)
		if inlineStyleRe.MatchString(s) || inlineScript.MatchString(s) || dataURLRe.MatchString(s) {
			t.Errorf("%s : style, script en ligne ou data: interdit", f)
		}
	}
	e := newEnv(t)
	gid := e.group("rendu", true)
	ids := e.addN(gid, "Hortense", 4)
	ck := e.adminCookie(gid)
	pages := map[string]string{}
	pages["formulaire"] = e.get("/rendu/admin/consultation", "fr", ck).Body.String()
	pages["erreurs"] = e.consult("rendu", "fr", ck, url.Values{}).Body.String()
	pages["avant tirage"] = e.consult("rendu", "fr", ck, url.Values{"person": {idStr(ids[0])}, "password": {e.secrets[gid]}}).Body.String()
	e.doDraw(gid)
	pages["résultat"] = e.consult("rendu", "fr", ck, url.Values{"person": {idStr(ids[0])}, "password": {e.secrets[gid]}}).Body.String()
	pages["journal"] = e.get("/rendu/consultations", "fr", e.participantCookie(gid, ids[1])).Body.String()
	pages["journal admin"] = e.get("/rendu/consultations", "fr", ck).Body.String()
	for name, body := range pages {
		if !strings.Contains(body, "<html") {
			t.Fatalf("%s : page non rendue", name)
		}
		if inlineStyleRe.MatchString(body) || inlineScript.MatchString(body) || dataURLRe.MatchString(body) {
			t.Errorf("%s : style, script en ligne ou data: interdit", name)
		}
	}
}
