package websec

import (
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"secretsanta/internal/shared/clock"
)

func wrapAll(p Policy, h http.Handler) http.Handler {
	return SecurityHeaders(p)(NewCSRF(p, CSRFOptions{}).Middleware(h))
}

func checkHeaders(t *testing.T, label string, h http.Header, https bool) {
	t.Helper()
	csp := h.Get("Content-Security-Policy")
	for _, d := range []string{
		"default-src 'self'", "script-src 'self'", "style-src 'self'", "form-action 'self'",
		"frame-ancestors 'none'", "base-uri 'none'", "object-src 'none'",
	} {
		if !strings.Contains(csp, d) {
			t.Fatalf("%s : CSP sans %q : %q", label, d, csp)
		}
	}
	for _, bad := range []string{"unsafe-inline", "unsafe-eval", "unsafe-hashes", "nonce-", "sha256-", "*", "data:", "http:", "https:"} {
		if strings.Contains(csp, bad) {
			t.Fatalf("%s : CSP permissive (%q) : %q", label, bad, csp)
		}
	}
	want := map[string]string{
		"X-Content-Type-Options": "nosniff",
		"Referrer-Policy":        "no-referrer",
		"X-Frame-Options":        "DENY",
	}
	for k, v := range want {
		if got := h.Get(k); got != v {
			t.Fatalf("%s : %s = %q, attendu %q", label, k, got, v)
		}
	}
	pp := h.Get("Permissions-Policy")
	for _, f := range []string{"camera=()", "microphone=()", "geolocation=()", "payment=()"} {
		if !strings.Contains(pp, f) {
			t.Fatalf("%s : Permissions-Policy sans %s : %q", label, f, pp)
		}
	}
	hsts := h.Get("Strict-Transport-Security")
	if https && !strings.HasPrefix(hsts, "max-age=") {
		t.Fatalf("%s : HSTS absent en https", label)
	}
	if !https && hsts != "" {
		t.Fatalf("%s : HSTS en http : %q", label, hsts)
	}
}

func TestSecurityHeadersOnEveryResponse(t *testing.T) {
	for _, pub := range []string{"https://noel.example.org", "http://localhost:8080"} {
		p := mustPolicy(t, pub)
		mux := http.NewServeMux()
		mux.HandleFunc("/ok", func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte("ok")) })
		mux.HandleFunc("/boom", func(w http.ResponseWriter, r *http.Request) { http.Error(w, "x", 500) })
		mux.HandleFunc("/redir", func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, "/ok", http.StatusSeeOther) })
		mux.Handle("/static/", http.StripPrefix("/static/", http.FileServer(http.Dir(t.TempDir()))))
		h := wrapAll(p, mux)
		reqs := []*http.Request{
			httptest.NewRequest(http.MethodGet, pub+"/ok", nil),
			httptest.NewRequest(http.MethodGet, pub+"/boom", nil),
			httptest.NewRequest(http.MethodGet, pub+"/redir", nil),
			httptest.NewRequest(http.MethodGet, pub+"/inconnu", nil),
			httptest.NewRequest(http.MethodGet, pub+"/static/absent.js", nil),
			httptest.NewRequest(http.MethodHead, pub+"/ok", nil),
			// Refus CSRF : les en-têtes sont aussi présents.
			httptest.NewRequest(http.MethodPost, pub+"/ok", nil),
		}
		for _, r := range reqs {
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, r)
			checkHeaders(t, pub+" "+r.Method+" "+r.URL.Path+" ("+http.StatusText(rec.Code)+")", rec.Header(), p.Secure)
		}
	}
}

func TestAllCookiesHardened(t *testing.T) {
	for _, tc := range []struct {
		pub    string
		secure bool
		prefix string
	}{
		{"https://noel.example.org", true, "__Host-"},
		{"http://localhost:8080", false, ""},
	} {
		p := mustPolicy(t, tc.pub)
		s := NewSessions(openDB(t), clock.NewFake(time.Date(2026, 12, 1, 8, 0, 0, 0, time.UTC)), p, SessionOptions{})
		mux := http.NewServeMux()
		mux.HandleFunc("/login/admin", func(w http.ResponseWriter, r *http.Request) {
			if _, err := s.Create(r.Context(), w, r, Identity{Kind: KindAdmin, GroupID: "g1"}); err != nil {
				t.Fatal(err)
			}
		})
		mux.HandleFunc("/login/participant", func(w http.ResponseWriter, r *http.Request) {
			if _, err := s.Create(r.Context(), w, r, Identity{Kind: KindParticipant, GroupID: "g1", Subject: "p1"}); err != nil {
				t.Fatal(err)
			}
		})
		h := wrapAll(p, s.Middleware(mux))
		want := map[string]http.SameSite{
			tc.prefix + "ss_admin":       http.SameSiteStrictMode,
			tc.prefix + "ss_participant": http.SameSiteLaxMode,
			tc.prefix + "ss_csrf":        http.SameSiteLaxMode,
		}
		seen := map[string]bool{}
		for _, path := range []string{"/login/admin", "/login/participant"} {
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, tc.pub+path, nil))
			for _, raw := range rec.Result().Header.Values("Set-Cookie") {
				if !strings.Contains(raw, "; HttpOnly") {
					t.Fatalf("%s : cookie sans HttpOnly : %s", tc.pub, raw)
				}
				if strings.Contains(raw, "; Secure") != tc.secure {
					t.Fatalf("%s : Secure incorrect : %s", tc.pub, raw)
				}
				if strings.Contains(raw, "Domain=") || !strings.Contains(raw, "Path=/") {
					t.Fatalf("%s : portée de cookie : %s", tc.pub, raw)
				}
			}
			for _, c := range rec.Result().Cookies() {
				ss, ok := want[c.Name]
				if !ok {
					t.Fatalf("%s : cookie inattendu %q", tc.pub, c.Name)
				}
				if c.SameSite != ss {
					t.Fatalf("%s : %s SameSite=%v, attendu %v", tc.pub, c.Name, c.SameSite, ss)
				}
				seen[c.Name] = true
			}
		}
		if len(seen) != len(want) {
			t.Fatalf("%s : cookies vus %v", tc.pub, seen)
		}
	}
}

func TestCheckTemplateDetectsInlineCode(t *testing.T) {
	bad := map[string]string{
		"script en ligne":         `<p>x</p><script>alert(1)</script>`,
		"script majuscules":       "<SCRIPT type=\"module\">\nimport x from '/static/x.js'\n</SCRIPT>",
		"script avec contenu":     `<script type="module" src="/static/a.js">alert(1)</script>`,
		"script externe":          `<script type="module" src="https://cdn.example/a.js"></script>`,
		"script classique":        `<script src="/static/a.js"></script>`,
		"script non fermé":        `<script type="module" src="/static/a.js">`,
		"onclick":                 `<button type="button" onclick="go()">Go</button>`,
		"onload majuscules":       `<body ONLOAD='x()'>`,
		"gestionnaire multiligne": "<img\n  src=\"/static/a.png\"\n  onerror=\"x()\">",
		"javascript:":             `<a href="javascript:void(0)">x</a>`,
	}
	for label, content := range bad {
		if len(CheckTemplate(label, content)) == 0 {
			t.Errorf("%s : non détecté", label)
		}
	}
	good := `<!doctype html><html lang="fr"><head>
<meta name="csrf-token" content="{{csrfToken}}">
<script type="module" src="/static/js/app.js"></script>
</head><body data-online="1"><form method="post">{{csrfField}}<button>Valider</button></form>
<p>Le bouton « once » ne contient ni onclick ni javascript.</p></body></html>`
	if p := CheckTemplate("bon", good); len(p) != 0 {
		t.Fatalf("faux positif : %v", p)
	}
}

// TestNoTemplateHasInlineScriptOrHandler parcourt tous les gabarits du dépôt
// (hors documentation de conception et dépendances).
func TestNoTemplateHasInlineScriptOrHandler(t *testing.T) {
	root := moduleRoot(t)
	skip := map[string]bool{".git": true, ".worktrees": true, "docs": true, "node_modules": true, "testdata": true}
	n := 0
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if path != root && skip[d.Name()] {
				return filepath.SkipDir
			}
			return nil
		}
		switch strings.ToLower(filepath.Ext(path)) {
		case ".html", ".tmpl", ".gohtml", ".gotmpl":
		default:
			return nil
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		n++
		rel, _ := filepath.Rel(root, path)
		for _, p := range CheckTemplate(rel, string(b)) {
			t.Error(p)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("%d gabarit(s) vérifié(s)", n)
}

func moduleRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("go.mod introuvable")
		}
		dir = parent
	}
}
