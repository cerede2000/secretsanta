package adminops_test

import (
	"context"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"

	"secretsanta/internal/adminops"
	"secretsanta/internal/websec"
	"secretsanta/web"
)

var (
	inlineStyleRe = regexp.MustCompile(`(?is)<[a-z][^>]*\sstyle\s*=|<style\b`)
	dataURLRe     = regexp.MustCompile(`(?i)data:`)
	inlineEventRe = regexp.MustCompile(`(?is)<[a-z][^>]*\son[a-z]+\s*=`)
)

// snapshot résume l'état que les routes modifiantes pourraient changer.
func (e *webEnv) snapshot(t *testing.T) string {
	t.Helper()
	last, err := e.ops.LastBackup(context.Background(), e.gid)
	if err != nil {
		t.Fatal(err)
	}
	_, verr := e.groups.VerifyAdminSecret(context.Background(), "famille", e.secret)
	return strings.Join([]string{
		strings.Repeat("g", e.count(t, `SELECT COUNT(*) FROM groups`)),
		strings.Repeat("p", e.count(t, `SELECT COUNT(*) FROM participants`)),
		strings.Repeat("s", e.count(t, `SELECT COUNT(*) FROM websec_sessions`)),
		map[bool]string{true: "sauvegarde", false: "aucune"}[last != nil],
		map[bool]string{true: "secret-ok", false: "secret-ko"}[verr == nil],
	}, "|")
}

// F10b.AC15 : chaque gestionnaire modifiant est enveloppé par
// RequireUnsafeMethod : GET, HEAD, OPTIONS et TRACE renvoient 405 sans rien
// modifier, en direct comme à travers la chaîne réelle.
func TestStateChangingHandlersRefuseSafeMethods(t *testing.T) {
	e := newWebEnv(t)
	before := e.snapshot(t)
	handlers := map[string]http.Handler{
		"/famille/admin/participants/import": e.pages.Import(),
		"/famille/admin/sauvegarde":          e.pages.Backup(),
		"/famille/admin/acces/reinitialiser": e.pages.Reset(),
		"/famille/admin/dupliquer":           e.pages.Duplicate(),
		"/famille/admin/supprimer":           e.pages.Delete(),
	}
	for path, h := range handlers {
		for _, m := range []string{http.MethodGet, http.MethodHead, http.MethodOptions, http.MethodTrace} {
			w := httptest.NewRecorder()
			r := httptest.NewRequest(m, path+"?confirm_name=Famille&purge=1", nil)
			r.SetPathValue("slug", "famille")
			for _, c := range e.cookies {
				r.AddCookie(c)
			}
			h.ServeHTTP(w, r)
			if w.Code != http.StatusMethodNotAllowed {
				t.Errorf("%s %s : %d, 405 attendu", m, path, w.Code)
			}
		}
		if w := e.get(path+"?confirm_name=Famille&purge=1", ""); w.Code != http.StatusMethodNotAllowed {
			t.Errorf("GET %s par la chaîne : %d", path, w.Code)
		}
	}
	if after := e.snapshot(t); after != before {
		t.Errorf("un GET a modifié l'état : %s -> %s", before, after)
	}
}

// Un POST sans jeton CSRF est refusé sans rien modifier.
func TestStateChangingRoutesNeedCSRF(t *testing.T) {
	e := newWebEnv(t)
	before := e.snapshot(t)
	for _, path := range []string{"/famille/admin/participants/import", "/famille/admin/sauvegarde",
		"/famille/admin/acces/reinitialiser", "/famille/admin/dupliquer", "/famille/admin/supprimer"} {
		form := url.Values{"confirm_name": {"Famille"}, "purge": {"1"}}
		w := e.request("POST", path, "", "application/x-www-form-urlencoded", strings.NewReader(form.Encode()), e.cookies)
		if w.Code != http.StatusForbidden {
			t.Errorf("POST %s sans jeton : %d, 403 attendu", path, w.Code)
		}
	}
	if after := e.snapshot(t); after != before {
		t.Errorf("un POST sans jeton a modifié l'état : %s -> %s", before, after)
	}
}

// Sans session administrateur du groupe, rien n'est servi ni modifié.
func TestPagesRequireAdminSession(t *testing.T) {
	e := newWebEnv(t)
	for _, path := range []string{"/famille/admin/donnees", "/famille/admin/participants/export"} {
		if w := e.request("GET", path, "", "", nil, nil); w.Code != http.StatusSeeOther || w.Header().Get("Location") != "/famille/admin/connexion" {
			t.Errorf("%s sans session : %d %q", path, w.Code, w.Header().Get("Location"))
		}
	}
	if w := e.get("/inconnu/admin/donnees", ""); w.Code != http.StatusNotFound {
		t.Errorf("groupe inconnu : %d", w.Code)
	}
}

// Parcours POST avec jeton : import, sauvegarde, réinitialisation,
// duplication et suppression confirmée.
func TestStateChangingRoutesWorkWithCSRF(t *testing.T) {
	e := newWebEnv(t)
	exp := e.get("/famille/admin/participants/export", "")
	if exp.Code != 200 || !strings.HasPrefix(exp.Header().Get("Content-Type"), "application/json") ||
		!strings.Contains(exp.Header().Get("Content-Disposition"), "attachment") {
		t.Fatalf("export : %d %v", exp.Code, exp.Header())
	}
	if w := e.postFile(t, "/famille/admin/participants/import", "", exp.Body.Bytes()); w.Code != http.StatusUnprocessableEntity ||
		!strings.Contains(w.Body.String(), `id="adminops-file-error"`) || !strings.Contains(w.Body.String(), `aria-invalid="true"`) {
		t.Errorf("import d'un doublon : %d", w.Code)
	}
	if w := e.postFile(t, "/famille/admin/participants/import", "", nil); w.Code != http.StatusUnprocessableEntity {
		t.Errorf("import sans fichier : %d", w.Code)
	}
	ok := `{"format":"secretsanta.participants","version":1,"participants":[{"first_name":"Luc","name":"Bernard","draw_role":"offre_plus"}],"households":[]}`
	if w := e.postFile(t, "/famille/admin/participants/import", "", []byte(ok)); w.Code != 200 || !strings.Contains(w.Body.String(), `role="status"`) {
		t.Errorf("import : %d", w.Code)
	}
	if n := e.count(t, `SELECT COUNT(*) FROM participants WHERE draw_role = 'offre_plus'`); n != 1 {
		t.Errorf("import non appliqué : %d", n)
	}
	b := e.postForm(t, "/famille/admin/sauvegarde", "", url.Values{})
	if b.Code != 200 || b.Header().Get("Content-Type") != adminops.BackupContentType || !strings.HasPrefix(b.Body.String(), "SQLite format 3") {
		t.Errorf("sauvegarde : %d %q", b.Code, b.Header().Get("Content-Type"))
	}
	d := e.postForm(t, "/famille/admin/dupliquer", "", url.Values{})
	if d.Code != 200 || !strings.Contains(d.Body.String(), "/famille-2027/admin/connexion") {
		t.Errorf("duplication : %d", d.Code)
	}
	bad := e.postForm(t, "/famille/admin/supprimer", "", url.Values{"confirm_name": {"famille"}})
	if bad.Code != http.StatusUnprocessableEntity || !strings.Contains(bad.Body.String(), `id="adminops-confirm-error"`) {
		t.Errorf("suppression mal confirmée : %d", bad.Code)
	}
	r := e.postForm(t, "/famille/admin/acces/reinitialiser", "", url.Values{})
	if r.Code != 200 || r.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("réinitialisation : %d", r.Code)
	}
	if _, err := e.groups.VerifyAdminSecret(context.Background(), "famille", e.secret); err == nil {
		t.Error("l'ancien secret fonctionne encore")
	}
	// La session est fermée : la suppression n'est plus possible sans se
	// reconnecter.
	if w := e.get("/famille/admin/donnees", ""); w.Code != http.StatusSeeOther {
		t.Errorf("session encore valide après réinitialisation : %d", w.Code)
	}
}

func TestDeleteRouteDeletesGroup(t *testing.T) {
	e := newWebEnv(t)
	w := e.postForm(t, "/famille/admin/supprimer", "", url.Values{"confirm_name": {"Famille"}, "purge": {"1"}})
	if w.Code != 200 {
		t.Fatalf("suppression : %d", w.Code)
	}
	if n := e.count(t, `SELECT COUNT(*) FROM groups WHERE slug = 'famille'`); n != 0 {
		t.Error("le groupe existe encore")
	}
	if n := e.count(t, `SELECT COUNT(*) FROM participants`); n != 0 {
		t.Errorf("%d participants restent", n)
	}
}

func TestTemplatesRespectStrictCSP(t *testing.T) {
	n := 0
	err := fs.WalkDir(web.Templates, "templates/adminops", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		n++
		b, err := fs.ReadFile(web.Templates, p)
		if err != nil {
			return err
		}
		src := string(b)
		if inlineStyleRe.MatchString(src) {
			t.Errorf("%s : style en ligne", p)
		}
		if dataURLRe.MatchString(src) {
			t.Errorf("%s : ressource data:", p)
		}
		if inlineEventRe.MatchString(src) {
			t.Errorf("%s : gestionnaire on* en ligne", p)
		}
		for _, problem := range websec.CheckTemplate(p, src) {
			t.Error(problem)
		}
		return nil
	})
	if err != nil || n == 0 {
		t.Fatalf("gabarits introuvables (%d) : %v", n, err)
	}
}

// Les rendus réels (chaque vue) n'ont ni style, ni data:, ni on* en ligne.
func TestRenderedPagesRespectStrictCSP(t *testing.T) {
	for name, body := range renderAllViews(t, "en") {
		if inlineStyleRe.MatchString(body) || dataURLRe.MatchString(body) || inlineEventRe.MatchString(body) {
			t.Errorf("%s : style, data: ou on* en ligne dans le rendu", name)
		}
		for _, problem := range websec.CheckTemplate(name, body) {
			t.Error(problem)
		}
	}
}
