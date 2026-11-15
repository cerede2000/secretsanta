package websec

import (
	"bytes"
	"html/template"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"
)

const testPublicURL = "https://noel.example.org"

func mustPolicy(t *testing.T, u string) Policy {
	t.Helper()
	p, err := NewPolicy(u)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

// csrfApp monte le middleware CSRF devant un gestionnaire qui compte les
// modifications et une page qui rend un formulaire avec csrfField.
type csrfApp struct {
	handler  http.Handler
	mutation int
}

func newCSRFApp(t *testing.T, opts CSRFOptions) *csrfApp {
	t.Helper()
	a := &csrfApp{}
	tmpl := template.Must(template.New("form").Funcs(TemplateFuncsPlaceholder()).Parse(
		`<form method="post" action="/change">{{csrfField}}</form><meta name="csrf-token" content="{{csrfToken}}">`))
	mux := http.NewServeMux()
	mux.HandleFunc("/form", func(w http.ResponseWriter, r *http.Request) {
		tm := template.Must(tmpl.Clone()).Funcs(TemplateFuncs(r))
		if err := tm.Execute(w, nil); err != nil {
			http.Error(w, err.Error(), 500)
		}
	})
	mux.Handle("/change", RequireUnsafeMethod(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		a.mutation++
		_, _ = io.WriteString(w, "ok")
	})))
	mux.HandleFunc("/unsubscribe", func(w http.ResponseWriter, r *http.Request) {
		a.mutation++
	})
	a.handler = NewCSRF(mustPolicy(t, testPublicURL), opts).Middleware(mux)
	return a
}

var (
	fieldRe = regexp.MustCompile(`name="csrf_token" value="([^"]+)"`)
	metaRe  = regexp.MustCompile(`name="csrf-token" content="([^"]+)"`)
)

// visit rend le formulaire et renvoie le cookie CSRF et le jeton du champ caché.
func (a *csrfApp) visit(t *testing.T) (*http.Cookie, string, string) {
	t.Helper()
	rec := httptest.NewRecorder()
	a.handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, testPublicURL+"/form", nil))
	if rec.Code != 200 {
		t.Fatalf("GET /form : %d %s", rec.Code, rec.Body)
	}
	var ck *http.Cookie
	for _, c := range rec.Result().Cookies() {
		if c.Name == "__Host-ss_csrf" {
			ck = c
		}
	}
	if ck == nil {
		t.Fatal("cookie CSRF absent")
	}
	if !ck.HttpOnly || !ck.Secure || ck.SameSite != http.SameSiteLaxMode || ck.Path != "/" || ck.Domain != "" {
		t.Fatalf("cookie CSRF mal durci : %+v", ck)
	}
	f := fieldRe.FindStringSubmatch(rec.Body.String())
	m := metaRe.FindStringSubmatch(rec.Body.String())
	if f == nil || m == nil {
		t.Fatalf("champ ou meta CSRF absent : %s", rec.Body)
	}
	return ck, f[1], m[1]
}

type reqOpt func(*http.Request)

func withOrigin(o string) reqOpt    { return func(r *http.Request) { r.Header.Set("Origin", o) } }
func withSFS(v string) reqOpt       { return func(r *http.Request) { r.Header.Set("Sec-Fetch-Site", v) } }
func withHeaderTok(v string) reqOpt { return func(r *http.Request) { r.Header.Set(CSRFHeaderName, v) } }

func (a *csrfApp) send(method, path string, ck *http.Cookie, formTok string, opts ...reqOpt) *httptest.ResponseRecorder {
	var body io.Reader
	// Le corps de formulaire n'est lu que pour POST, PUT et PATCH (comme
	// net/http) : pour les autres méthodes, le jeton passe par l'en-tête.
	if formTok != "" && method != http.MethodPost && method != http.MethodPut && method != http.MethodPatch {
		opts = append([]reqOpt{withHeaderTok(formTok)}, opts...)
		formTok = ""
	}
	if formTok != "" {
		body = strings.NewReader(url.Values{CSRFFieldName: {formTok}, "x": {"1"}}.Encode())
	}
	r := httptest.NewRequest(method, testPublicURL+path, body)
	if body != nil {
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	if ck != nil {
		r.AddCookie(&http.Cookie{Name: ck.Name, Value: ck.Value})
	}
	for _, o := range opts {
		o(r)
	}
	rec := httptest.NewRecorder()
	a.handler.ServeHTTP(rec, r)
	return rec
}

func alter(tok string) string {
	b := []byte(tok)
	if b[0] == 'A' {
		b[0] = 'B'
	} else {
		b[0] = 'A'
	}
	return string(b)
}

func expect403(t *testing.T, label string, rec *httptest.ResponseRecorder) {
	t.Helper()
	if rec.Code != http.StatusForbidden {
		t.Fatalf("%s : statut %d, attendu 403", label, rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "La requête a été refusée") {
		t.Fatalf("%s : message français absent : %q", label, rec.Body)
	}
}

func TestCSRFRejectsMutatingRequestsWithoutValidTokenOrOrigin(t *testing.T) {
	for _, method := range []string{http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete, "PROPFIND"} {
		t.Run(method, func(t *testing.T) {
			a := newCSRFApp(t, CSRFOptions{})
			ck, tok, _ := a.visit(t)
			_, otherTok, _ := newCSRFApp(t, CSRFOptions{}).visit(t)
			same := withOrigin(testPublicURL)
			cases := []struct {
				label string
				rec   *httptest.ResponseRecorder
			}{
				{"sans jeton", a.send(method, "/change", ck, "", same)},
				{"sans cookie", a.send(method, "/change", nil, tok, same)},
				{"jeton d'un autre cookie", a.send(method, "/change", ck, otherTok, same)},
				{"jeton altéré", a.send(method, "/change", ck, alter(tok), same)},
				{"jeton brut = cookie", a.send(method, "/change", ck, ck.Value, same)},
				{"origine étrangère", a.send(method, "/change", ck, tok, withOrigin("https://evil.example"))},
				{"sous-domaine", a.send(method, "/change", ck, tok, withOrigin("https://x.noel.example.org"))},
				{"http au lieu de https", a.send(method, "/change", ck, tok, withOrigin("http://noel.example.org"))},
				{"cross-site", a.send(method, "/change", ck, tok, withSFS("cross-site"))},
				{"same-site", a.send(method, "/change", ck, tok, withSFS("same-site"))},
				{"origine valide mais cross-site", a.send(method, "/change", ck, tok, same, withSFS("cross-site"))},
				{"aucune origine attestée", a.send(method, "/change", ck, tok)},
				{"Origin null seul", a.send(method, "/change", ck, tok, withOrigin("null"))},
			}
			for _, c := range cases {
				expect403(t, c.label, c.rec)
			}
			if a.mutation != 0 {
				t.Fatalf("%d modification(s) malgré le refus", a.mutation)
			}
		})
	}
}

func TestCSRFAcceptsSameRequestWithValidTokenAndOrigin(t *testing.T) {
	for _, method := range []string{http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete} {
		t.Run(method, func(t *testing.T) {
			a := newCSRFApp(t, CSRFOptions{})
			ck, tok, meta := a.visit(t)
			oks := []*httptest.ResponseRecorder{
				// Formulaire, Origin exacte.
				a.send(method, "/change", ck, tok, withOrigin(testPublicURL)),
				// Formulaire, Referrer-Policy no-referrer : Origin null + Sec-Fetch-Site.
				a.send(method, "/change", ck, tok, withOrigin("null"), withSFS("same-origin")),
				// Appel JavaScript : en-tête X-CSRF-Token issu de la meta.
				a.send(method, "/change", ck, "", withHeaderTok(meta), withSFS("same-origin"), withOrigin(testPublicURL)),
				// Origin en casse différente.
				a.send(method, "/change", ck, tok, withOrigin("HTTPS://Noel.Example.org")),
			}
			for i, rec := range oks {
				if rec.Code != 200 {
					t.Fatalf("cas %d : statut %d %s", i, rec.Code, rec.Body)
				}
			}
			if a.mutation != len(oks) {
				t.Fatalf("modifications = %d, attendu %d", a.mutation, len(oks))
			}
		})
	}
}

func TestCSRFTokenIsMaskedPerRender(t *testing.T) {
	a := newCSRFApp(t, CSRFOptions{})
	ck, tok1, _ := a.visit(t)
	// Second rendu avec le même cookie : jeton différent, tous deux valides.
	r := httptest.NewRequest(http.MethodGet, testPublicURL+"/form", nil)
	r.AddCookie(&http.Cookie{Name: ck.Name, Value: ck.Value})
	rec := httptest.NewRecorder()
	a.handler.ServeHTTP(rec, r)
	if len(rec.Result().Cookies()) != 0 {
		t.Fatal("cookie CSRF réémis alors qu'il est valide")
	}
	tok2 := fieldRe.FindStringSubmatch(rec.Body.String())[1]
	if tok1 == tok2 || strings.Contains(tok1, ck.Value) {
		t.Fatal("jeton non masqué")
	}
	for _, tok := range []string{tok1, tok2} {
		if rec := a.send(http.MethodPost, "/change", ck, tok, withOrigin(testPublicURL)); rec.Code != 200 {
			t.Fatalf("jeton refusé : %d", rec.Code)
		}
	}
}

func TestGETNeverModifiesState(t *testing.T) {
	a := newCSRFApp(t, CSRFOptions{})
	ck, tok, _ := a.visit(t)
	for _, m := range []string{http.MethodGet, http.MethodHead, http.MethodOptions} {
		// Même avec jeton et origine valides, un gestionnaire modifiant
		// n'est jamais atteint par une méthode sûre.
		r := httptest.NewRequest(m, testPublicURL+"/change?"+CSRFFieldName+"="+url.QueryEscape(tok), nil)
		r.AddCookie(&http.Cookie{Name: ck.Name, Value: ck.Value})
		r.Header.Set("Origin", testPublicURL)
		rec := httptest.NewRecorder()
		a.handler.ServeHTTP(rec, r)
		if rec.Code != http.StatusMethodNotAllowed {
			t.Fatalf("%s /change : statut %d, attendu 405", m, rec.Code)
		}
	}
	if a.mutation != 0 {
		t.Fatalf("une méthode sûre a modifié l'état (%d)", a.mutation)
	}
}

func TestCSRFExemptOnlyDeclaredRoutes(t *testing.T) {
	a := newCSRFApp(t, CSRFOptions{Exempt: func(r *http.Request) bool { return r.URL.Path == "/unsubscribe" }})
	if rec := a.send(http.MethodPost, "/unsubscribe", nil, "", withOrigin("null")); rec.Code != 200 {
		t.Fatalf("route exemptée refusée : %d", rec.Code)
	}
	expect403(t, "route non exemptée", a.send(http.MethodPost, "/change", nil, "", withOrigin("null")))
	if a.mutation != 1 {
		t.Fatalf("modifications = %d", a.mutation)
	}
	// Sans déclaration, la même route est protégée.
	b := newCSRFApp(t, CSRFOptions{})
	expect403(t, "sans exemption", b.send(http.MethodPost, "/unsubscribe", nil, "", withOrigin("null")))
}

func TestCSRFOnRejectAndMissingMiddleware(t *testing.T) {
	called := false
	a := newCSRFApp(t, CSRFOptions{OnReject: func(w http.ResponseWriter, r *http.Request, reason error) {
		called = reason == ErrCSRFToken
		w.WriteHeader(http.StatusForbidden)
	}})
	ck, _, _ := a.visit(t)
	if rec := a.send(http.MethodPost, "/change", ck, "", withOrigin(testPublicURL)); rec.Code != 403 || !called {
		t.Fatalf("OnReject non appelé : %d %v", rec.Code, called)
	}
	// Sans middleware, csrfField échoue au lieu de rendre un champ vide.
	tm := template.Must(template.New("x").Funcs(TemplateFuncs(httptest.NewRequest("GET", "/", nil))).Parse(`{{csrfField}}`))
	if err := tm.Execute(&bytes.Buffer{}, nil); err == nil {
		t.Fatal("csrfField sans middleware devrait échouer")
	}
}

func TestCSRFCookieNotSecureOverHTTP(t *testing.T) {
	h := NewCSRF(mustPolicy(t, "http://localhost:8080"), CSRFOptions{}).Middleware(http.NotFoundHandler())
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "http://localhost:8080/", nil))
	cks := rec.Result().Cookies()
	if len(cks) != 1 || cks[0].Name != "ss_csrf" || cks[0].Secure || !cks[0].HttpOnly {
		t.Fatalf("cookie CSRF en http : %+v", cks)
	}
	// L'origine http://localhost:8080 est acceptée en http.
	p := mustPolicy(t, "http://localhost:8080/")
	if p.Origin != "http://localhost:8080" {
		t.Fatalf("origine %q", p.Origin)
	}
	if p := mustPolicy(t, "https://Noel.Example.org:443/sous/"); p.Origin != testPublicURL || !p.Secure {
		t.Fatalf("politique %+v", p)
	}
}
