package negotiate

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type fakeMatcher map[string]bool

func (f fakeMatcher) Match(tag string) (string, bool) {
	tag = strings.ToLower(tag)
	for tag != "" {
		if f[tag] {
			return tag, true
		}
		i := strings.LastIndex(tag, "-")
		if i < 0 {
			break
		}
		tag = tag[:i]
	}
	return "", false
}

var loaded = fakeMatcher{"fr": true, "en": true, "de": true}

func TestChoosePriority(t *testing.T) {
	cases := []struct {
		name string
		in   Input
		want string
	}{
		{"cookie d'abord", Input{Cookie: "en", AcceptLanguage: "de", GroupLang: "de", InstanceLang: "fr"}, "en"},
		{"cookie inconnu, navigateur", Input{Cookie: "xx", AcceptLanguage: "de", GroupLang: "en", InstanceLang: "fr"}, "de"},
		{"qualités", Input{AcceptLanguage: "fr;q=0.3, de;q=0.9, en;q=0.5"}, "de"},
		{"q=0 exclu", Input{AcceptLanguage: "de;q=0, en;q=0.1", InstanceLang: "fr"}, "en"},
		{"région retombe sur la langue", Input{AcceptLanguage: "en-US,fr;q=0.5"}, "en"},
		{"navigateur inconnu, groupe", Input{AcceptLanguage: "ja, zh;q=0.5", GroupLang: "en", InstanceLang: "fr"}, "en"},
		{"groupe inconnu, instance", Input{AcceptLanguage: "ja", GroupLang: "xx", InstanceLang: "de"}, "de"},
		{"joker ignoré", Input{AcceptLanguage: "*", InstanceLang: "fr"}, "fr"},
		{"rien : instance", Input{InstanceLang: "fr"}, "fr"},
		{"en-têtes absurdes", Input{AcceptLanguage: ";;,,q=,;q=x,en;q=abc,", InstanceLang: "fr"}, "fr"},
	}
	for _, c := range cases {
		if got := Choose(loaded, c.in); got != c.want {
			t.Errorf("%s : %q, attendu %q", c.name, got, c.want)
		}
	}
}

type fakeGroup struct {
	lang  string
	calls int
	slug  string
}

func (f *fakeGroup) GroupLanguage(_ context.Context, slug string) (string, bool) {
	f.calls++
	f.slug = slug
	return f.lang, f.lang != ""
}

func run(t *testing.T, g GroupLanguage, path string, hdr map[string]string, cookie string) string {
	t.Helper()
	var got string
	h := Middleware(loaded, "fr", g)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = Lang(r.Context(), "?")
	}))
	r := httptest.NewRequest(http.MethodGet, path, nil)
	for k, v := range hdr {
		r.Header.Set(k, v)
	}
	if cookie != "" {
		r.AddCookie(&http.Cookie{Name: CookieName, Value: cookie})
	}
	h.ServeHTTP(httptest.NewRecorder(), r)
	return got
}

func TestMiddleware(t *testing.T) {
	g := &fakeGroup{lang: "en"}
	if got := run(t, g, "/noel/x", nil, ""); got != "en" || g.slug != "noel" {
		t.Fatalf("groupe : %q (slug %q)", got, g.slug)
	}
	if got := run(t, g, "/noel", map[string]string{"Accept-Language": "de"}, ""); got != "de" {
		t.Fatalf("navigateur avant groupe : %q", got)
	}
	g.calls = 0
	if got := run(t, g, "/noel", nil, "de"); got != "de" || g.calls != 0 {
		t.Fatalf("cookie : %q, groupe interrogé %d fois", got, g.calls)
	}
	if got := run(t, nil, "/", nil, ""); got != "fr" {
		t.Fatalf("instance : %q", got)
	}
	if got := run(t, g, "/", nil, "zz"); got != "fr" {
		t.Fatalf("cookie inconnu, sans slug : %q", got)
	}
	if Lang(context.Background(), "fr") != "fr" {
		t.Fatal("repli du contexte")
	}
}
