package grouppages

import (
	"context"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"

	"secretsanta/internal/groups"
	"secretsanta/internal/websec"
	"secretsanta/web"
)

type countingService struct{ creates, reads int }

func (c *countingService) PublicPage(context.Context, string) (groups.PublicView, error) {
	c.reads++
	return groups.PublicView{Slug: "g", Name: "G"}, nil
}

func (c *countingService) Create(context.Context, groups.CreateInput) (*groups.CreateResult, error) {
	c.creates++
	return &groups.CreateResult{}, nil
}

func TestCreateSubmitRefusesSafeMethods(t *testing.T) {
	svc := &countingService{}
	p, err := New(Deps{Groups: svc, Render: func(w http.ResponseWriter, r *http.Request, status int, name string, data any) {
		w.WriteHeader(status)
	}})
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range []string{http.MethodGet, http.MethodHead, http.MethodOptions, http.MethodTrace} {
		w := httptest.NewRecorder()
		p.CreateSubmit().ServeHTTP(w, httptest.NewRequest(m, "/creer?password=x&name=n&slug=abc", nil))
		if w.Code != http.StatusMethodNotAllowed {
			t.Errorf("%s : statut %d, 405 attendu", m, w.Code)
		}
	}
	if svc.creates != 0 {
		t.Errorf("Create appelé %d fois par une méthode sûre", svc.creates)
	}
}

// Sur le mux réel, un GET sur /creer sert seulement le formulaire et ne crée
// aucun groupe, même avec tous les champs en paramètres de requête.
func TestGetNeverCreates(t *testing.T) {
	e := newEnv(t)
	w := e.do("GET", "/creer?password="+testInstancePassword+"&name=Test&slug=via-get", "", nil, nil)
	if w.Code != 200 {
		t.Fatalf("GET /creer : %d", w.Code)
	}
	if _, err := e.svc.PublicPage(context.Background(), "via-get"); err == nil {
		t.Error("un GET a créé un groupe")
	}
	if w := e.do("GET", "/via-get", "", nil, nil); w.Code != 404 {
		t.Errorf("groupe inconnu : %d, 404 attendu", w.Code)
	}
}

// Un POST sans jeton CSRF est refusé par le middleware avant le service.
func TestPostWithoutTokenRefused(t *testing.T) {
	e := newEnv(t)
	w := e.do("POST", "/creer", "", validForm("no-token"), nil)
	if w.Code != http.StatusForbidden {
		t.Errorf("POST sans jeton : %d, 403 attendu", w.Code)
	}
	if _, err := e.svc.PublicPage(context.Background(), "no-token"); err == nil {
		t.Error("un POST sans jeton a créé un groupe")
	}
}

var (
	inlineStyleRe = regexp.MustCompile(`(?is)<[a-z][^>]*\sstyle\s*=|<style\b`)
	dataURLRe     = regexp.MustCompile(`(?i)data:`)
)

func TestTemplatesRespectStrictCSP(t *testing.T) {
	n := 0
	err := fs.WalkDir(web.Templates, "templates/groups", func(p string, d fs.DirEntry, err error) error {
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
			t.Errorf("%s : attribut ou bloc style en ligne", p)
		}
		if dataURLRe.MatchString(src) {
			t.Errorf("%s : ressource data:", p)
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

func TestRenderedPagesRespectStrictCSP(t *testing.T) {
	e := newEnv(t)
	for name, body := range pagesEnglish(t, e) {
		if inlineStyleRe.MatchString(body) {
			t.Errorf("%s : style en ligne dans le rendu", name)
		}
		if dataURLRe.MatchString(body) {
			t.Errorf("%s : data: dans le rendu", name)
		}
		for _, problem := range websec.CheckTemplate(name, body) {
			t.Error(problem)
		}
		if strings.Contains(body, "http://") && strings.Contains(body, "src=\"http") {
			t.Errorf("%s : ressource distante", name)
		}
	}
}
