package letterpages

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

func TestStateChangingRoutesRejectGet(t *testing.T) {
	e := newEnv(t)
	a := e.add(t, "Alice", participants.RoleNormal)
	c := e.login(t, a)
	idea, err := e.letters.AddIdea(ctx, e.gid, a, ideaIn("Chaussettes"))
	if err != nil {
		t.Fatal(err)
	}
	before := e.letterVersion(t, a)
	n := 0
	for _, r := range e.pages.Routes() {
		if !r.Modifies {
			continue
		}
		n++
		_, path, _ := strings.Cut(r.Pattern, " ")
		path = strings.NewReplacer("{slug}", "noel", "{idee}", itoa(idea)).Replace(path)
		for _, m := range []string{http.MethodGet, http.MethodHead, http.MethodOptions} {
			req := httptest.NewRequest(m, path, nil)
			req.SetPathValue("slug", "noel")
			req.SetPathValue("idee", itoa(idea))
			req.AddCookie(c)
			rec := httptest.NewRecorder()
			r.Handler.ServeHTTP(rec, req)
			if rec.Code != http.StatusMethodNotAllowed {
				t.Errorf("%s %s : 405 attendu, %d", m, r.Pattern, rec.Code)
			}
		}
	}
	if n < 3 {
		t.Fatalf("routes modifiantes : %d", n)
	}
	l, _ := e.letters.Get(ctx, e.gid, a)
	if len(l.Ideas) != 1 || l.Version != before {
		t.Fatal("un GET a modifié la lettre")
	}
}

func TestRoutesDeclareTheirMethodsConsistently(t *testing.T) {
	e := newEnv(t)
	for _, r := range e.pages.Routes() {
		if strings.HasPrefix(r.Pattern, "POST ") != r.Modifies {
			t.Errorf("%s : Modifies=%v incohérent", r.Pattern, r.Modifies)
		}
	}
}

func TestPostWithoutCSRFTokenIsRefused(t *testing.T) {
	e := newEnv(t)
	a := e.add(t, "Alice", participants.RoleNormal)
	req := httptest.NewRequest(http.MethodPost, "/noel/lettre", strings.NewReader("intro=x"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Sec-Fetch-Site", "same-origin")
	req.AddCookie(e.login(t, a))
	rec := httptest.NewRecorder()
	e.handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("POST sans jeton : %d", rec.Code)
	}
	if l, _ := e.letters.Get(ctx, e.gid, a); l.Intro != "" {
		t.Fatal("écriture sans jeton CSRF")
	}
}

var (
	inlineStyleRe = regexp.MustCompile(`(?i)<style[\s>]|\sstyle\s*=`)
	inlineScript  = regexp.MustCompile(`(?i)<script(\s[^>]*)?>[^<]|\son[a-z]+\s*=`)
	dataURLRe     = regexp.MustCompile(`(?i)data:[a-z]+/|url\(\s*["']?data:`)
)

func TestTemplatesAndAssetsHaveNoInlineStyleScriptOrDataURL(t *testing.T) {
	count := 0
	for _, dir := range []string{"templates/letter", "static/css/letter", "static/js/letter"} {
		var fsys fs.FS = web.Templates
		if dir != "templates/letter" {
			fsys = web.Static
		}
		entries, err := fs.ReadDir(fsys, dir)
		if err != nil {
			continue // dossier de scripts facultatif
		}
		for _, en := range entries {
			b, err := fs.ReadFile(fsys, dir+"/"+en.Name())
			if err != nil {
				t.Fatal(err)
			}
			src, name := string(b), dir+"/"+en.Name()
			if inlineStyleRe.MatchString(src) || inlineScript.MatchString(src) || dataURLRe.MatchString(src) {
				t.Errorf("%s : contenu interdit par la CSP", name)
			}
			if strings.HasPrefix(dir, "templates") {
				for _, p := range websec.CheckTemplate(name, src) {
					t.Errorf("%s : %s", name, p)
				}
			}
			count++
		}
	}
	if count < 3 {
		t.Fatalf("fichiers vérifiés : %d", count)
	}
}

func TestRenderedPagesHaveNoInlineStyleOrDataURL(t *testing.T) {
	e := newEnv(t)
	a := e.add(t, "Alice", participants.RoleNormal)
	e.add(t, "Bobby", participants.RoleNormal)
	e.add(t, "Carla", participants.RoleNormal)
	if _, err := e.letters.AddIdea(ctx, e.gid, a, ideaIn("Mug")); err != nil {
		t.Fatal(err)
	}
	ps := e.revealed(t)
	var pair rawPair
	for _, p := range ps {
		if p.Receiver == a {
			pair = p
		}
	}
	for path, c := range map[string]*http.Cookie{
		"/noel/lettre":                        e.login(t, a),
		"/noel/lettre?bienvenue=1":            e.login(t, a),
		"/noel/lettre-recue/" + itoa(pair.ID): e.login(t, pair.Giver),
	} {
		rec := e.get(t, path, c)
		if rec.Code != 200 {
			t.Fatalf("%s : %d", path, rec.Code)
		}
		body := rec.Body.String()
		if inlineStyleRe.MatchString(body) || dataURLRe.MatchString(body) || strings.Contains(body, "<script>") {
			t.Errorf("%s : contenu interdit par la CSP", path)
		}
	}
}
