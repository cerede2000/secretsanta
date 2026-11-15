package web

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"secretsanta/internal/i18n/catalog"
)

func TestToIntAcceptsEveryNumericKindAndRejectsTheRest(t *testing.T) {
	cases := []struct {
		in   any
		want int
	}{
		{int(3), 3}, {int8(4), 4}, {int16(5), 5}, {int32(6), 6}, {int64(7), 7},
		{uint(8), 8}, {uint8(9), 9}, {uint16(10), 10}, {uint32(11), 11}, {uint64(12), 12},
		{float32(13.9), 13}, {float64(14.9), 14},
		{"15", 15}, {"pas un nombre", 0}, {"", 0},
		{nil, 0}, {true, 0}, {[]int{1}, 0}, {struct{}{}, 0},
	}
	for _, c := range cases {
		if got := toInt(c.in); got != c.want {
			t.Errorf("toInt(%#v) = %d, attendu %d", c.in, got, c.want)
		}
	}
}

func TestFuncsPlaceholderRendersKeysUntilBound(t *testing.T) {
	f := FuncsPlaceholder()
	for _, name := range []string{"t", "tn", "langs", "langCurrent", "langNext"} {
		if f[name] == nil {
			t.Fatalf("fonction %q absente", name)
		}
	}
	if got := f["t"].(func(string, ...any) string)("core.hello", "name", "x"); got != "core.hello" {
		t.Errorf("t : %q", got)
	}
	if got := f["tn"].(func(string, any, ...any) string)("core.items", 3); got != "core.items" {
		t.Errorf("tn : %q", got)
	}
	if got := f["langs"].(func() []catalog.Lang)(); got != nil {
		t.Errorf("langs : %v", got)
	}
	if got := f["langCurrent"].(func() string)(); got != "" {
		t.Errorf("langCurrent : %q", got)
	}
	if got := f["langNext"].(func() string)(); got != "/" {
		t.Errorf("langNext : %q", got)
	}
}

// panicTranslator panique dans T et TN : le rendu doit rendre la clé, jamais
// propager la panique.
type panicTranslator struct{ Translator }

func (panicTranslator) T(string, string, catalog.Params) string { panic("T cassé") }
func (panicTranslator) TN(string, string, int, catalog.Params) string {
	panic("TN cassé")
}

func TestFuncsRecoverFromTranslatorPanics(t *testing.T) {
	r := reqLang(http.MethodGet, "/x", "fr")
	f := Funcs(panicTranslator{testCatalog(t)}, r)
	if got := f["t"].(func(string, ...any) string)("core.hello", "name", "x"); got != "core.hello" {
		t.Errorf("t : %q", got)
	}
	if got := f["tn"].(func(string, any, ...any) string)("core.items", "2"); got != "core.items" {
		t.Errorf("tn : %q", got)
	}
}

func TestFuncsExposeLanguagesCurrentAndSafeNext(t *testing.T) {
	r := reqLang(http.MethodGet, "/groupe/page?a=1&b=2", "en")
	f := Funcs(testCatalog(t), r)
	if got := f["langCurrent"].(func() string)(); got != "en" {
		t.Errorf("langCurrent : %q", got)
	}
	if got := f["langNext"].(func() string)(); got != "/groupe/page?a=1&b=2" {
		t.Errorf("langNext : %q", got)
	}
	if n := len(f["langs"].(func() []catalog.Lang)()); n != 3 {
		t.Errorf("langs : %d langues, attendu 3", n)
	}
	if got := f["tn"].(func(string, any, ...any) string)("core.items", int64(1)); got != "1 gift" {
		t.Errorf("tn singulier : %q", got)
	}
}

func TestSafeNextOnlyKeepsLocalPaths(t *testing.T) {
	ok := []string{"/", "/a/b", "/a?x=1&y=2", "/a#frag"}
	for _, s := range ok {
		if got := SafeNext(s); got != s {
			t.Errorf("SafeNext(%q) = %q", s, got)
		}
	}
	bad := []string{
		"", "a/b", "//evil.example", "/\\evil.example", "http://evil.example/",
		"/a\nb", "/a\rb", "/a\tb", "/a\x01b", "/a\x7fb", "/%zz", "/a b:%",
	}
	for _, s := range bad {
		if got := SafeNext(s); got != "/" {
			t.Errorf("SafeNext(%q) = %q, attendu /", s, got)
		}
	}
}

func TestSelectorRefusesUnparsableBody(t *testing.T) {
	s := &Selector{Cat: testCatalog(t)}
	req := httptest.NewRequest(http.MethodPost, "/langue", strings.NewReader("lang=%zz"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("%d", rec.Code)
	}
	if len(rec.Result().Cookies()) != 0 {
		t.Fatal("aucun cookie ne doit être posé sur une requête invalide")
	}
}
