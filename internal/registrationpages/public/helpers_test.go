package public

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"secretsanta/internal/groups"
	"secretsanta/internal/i18n/catalog"
	"secretsanta/internal/i18n/negotiate"
	"secretsanta/internal/registration/code"
	"secretsanta/internal/registration/invite"
	"secretsanta/internal/registration/verify"
	"secretsanta/internal/websec"
	"secretsanta/web"
)

const (
	publicURL = "http://127.0.0.1:8080"
	joinToken = "q3Vb0f9xZ2kLmN8pR1sT4w"
	joinPath  = "/noel/rejoindre/" + joinToken
)

// fakeGroups sert un seul groupe, "noel".
type fakeGroups struct{ g *groups.Group }

func (f *fakeGroups) BySlug(_ context.Context, slug string) (*groups.Group, error) {
	if f.g == nil || slug != f.g.Slug {
		return nil, nil
	}
	cp := *f.g
	return &cp, nil
}

// fakeInvites simule le service d'invitation et enregistre les appels.
type fakeInvites struct {
	mu          sync.Mutex
	closed      invite.ClosedReason
	roleChoice  bool
	result      *invite.Result
	submissions []invite.Submission
	verifyErr   error
}

func (f *fakeInvites) Open(_ context.Context, slug, tok string) (*invite.Invite, error) {
	if slug != "noel" || tok != joinToken {
		return nil, invite.ErrNeutral
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	return &invite.Invite{GroupID: 1, Slug: "noel", Name: "Noël en famille", Closed: f.closed, RoleChoice: f.roleChoice}, nil
}

func (f *fakeInvites) Submit(_ context.Context, slug, tok string, sub invite.Submission) (*invite.Result, error) {
	if slug != "noel" || tok != joinToken {
		return nil, invite.ErrNeutral
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.submissions = append(f.submissions, sub)
	if f.result != nil {
		return f.result, nil
	}
	return &invite.Result{Outcome: invite.OutcomeCodeSent, Ref: "ref-1"}, nil
}

func (f *fakeInvites) VerificationAllowed(context.Context, int64) error { return f.verifyErr }

func (f *fakeInvites) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.submissions)
}

// fakeVerify simule la vérification : le code "ABC123" est le bon.
type fakeVerify struct {
	mu        sync.Mutex
	verified  int
	confirmed int
	already   bool
}

func (f *fakeVerify) VerifyCode(_ context.Context, _ int64, ref, input string) (*verify.Success, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if ref != "ref-1" || input != "ABC123" {
		return nil, code.ErrRefused
	}
	f.verified++
	return &verify.Success{GroupID: 1, Slug: "noel", FirstName: "Camille", AlreadyVerified: f.already}, nil
}

func (f *fakeVerify) PreviewLink(_ context.Context, _ int64, tok string) (code.Preview, error) {
	if tok != "lien" {
		return code.Preview{}, code.ErrRefused
	}
	return code.Preview{RegistrationID: 1, Code: "ABC123"}, nil
}

func (f *fakeVerify) ConfirmLink(_ context.Context, _ int64, tok string) (*verify.Success, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if tok != "lien" {
		return nil, code.ErrRefused
	}
	f.confirmed++
	return &verify.Success{GroupID: 1, Slug: "noel", FirstName: "Camille"}, nil
}

func (f *fakeVerify) total() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.verified + f.confirmed
}

type env struct {
	invites *fakeInvites
	verify  *fakeVerify
	group   *groups.Group
	pages   *Pages
	cat     *catalog.Catalog
	handler http.Handler
}

func realCatalog(t *testing.T) *catalog.Catalog {
	t.Helper()
	cat, err := catalog.Load(web.I18n, catalog.Options{DefaultLang: "fr", Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})
	if err != nil {
		t.Fatal(err)
	}
	return cat
}

func newEnv(t *testing.T) *env {
	t.Helper()
	e := &env{
		invites: &fakeInvites{},
		verify:  &fakeVerify{},
		group:   &groups.Group{ID: 1, Slug: "noel", Name: "Noël en famille"},
		cat:     realCatalog(t),
	}
	var err error
	e.pages, err = New(Options{
		Groups: &fakeGroupsRef{e}, Invites: e.invites, Verify: e.verify, Translator: e.cat,
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	e.pages.Register(mux)
	pol, err := websec.NewPolicy(publicURL)
	if err != nil {
		t.Fatal(err)
	}
	csrf := websec.NewCSRF(pol, websec.CSRFOptions{}).Middleware(mux)
	e.handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		lang := r.Header.Get("X-Test-Lang")
		if lang == "" {
			lang = "fr"
		}
		csrf.ServeHTTP(w, r.WithContext(negotiate.WithLang(r.Context(), lang)))
	})
	return e
}

// fakeGroupsRef relit le groupe de l'environnement à chaque appel : un test
// peut en changer le budget.
type fakeGroupsRef struct{ e *env }

func (f *fakeGroupsRef) BySlug(ctx context.Context, slug string) (*groups.Group, error) {
	return (&fakeGroups{g: f.e.group}).BySlug(ctx, slug)
}

var csrfRe = regexp.MustCompile(`name="csrf_token" value="([^"]+)"`)

// do envoie une requête ; les POST portent le jeton et le cookie CSRF d'une
// visite préalable du formulaire.
func (e *env) do(t *testing.T, method, target string, form url.Values, lang string) *httptest.ResponseRecorder {
	t.Helper()
	var cookies []*http.Cookie
	if method == http.MethodPost {
		get := httptest.NewRequest(http.MethodGet, joinPath, nil)
		rec := httptest.NewRecorder()
		e.handler.ServeHTTP(rec, get)
		m := csrfRe.FindStringSubmatch(rec.Body.String())
		if m == nil {
			t.Fatalf("jeton CSRF introuvable : %d %s", rec.Code, rec.Body.String())
		}
		cookies = rec.Result().Cookies()
		cp := url.Values{}
		for k, v := range form {
			cp[k] = append([]string(nil), v...)
		}
		cp.Set(websec.CSRFFieldName, m[1])
		form = cp
	}
	var body io.Reader
	if form != nil {
		body = strings.NewReader(form.Encode())
	}
	req := httptest.NewRequest(method, target, body)
	if form != nil {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	req.Header.Set("Sec-Fetch-Site", "same-origin")
	req.Header.Set("X-Test-Lang", lang)
	req.RemoteAddr = "198.51.100.7:1234"
	for _, c := range cookies {
		req.AddCookie(c)
	}
	rec := httptest.NewRecorder()
	e.handler.ServeHTTP(rec, req)
	return rec
}

func cents(v int64) *int64 { return &v }

func at(y int, m time.Month, d, h int) *time.Time {
	t := time.Date(y, m, d, h, 0, 0, 0, time.UTC)
	return &t
}

func codeForm(ref, input string) url.Values {
	return url.Values{"ref": {ref}, "code": {input}}
}
