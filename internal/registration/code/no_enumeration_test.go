package code

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"secretsanta/internal/shared/errs"
)

// F11.AC12 : un code faux, un code expiré et un nombre de tentatives épuisé
// produisent la même réponse sans dire lequel.

func refusalOf(t *testing.T, err error) *errs.Error {
	t.Helper()
	var e *errs.Error
	if !errors.As(err, &e) {
		t.Fatalf("erreur non typée : %v", err)
	}
	return e
}

func TestRefusalsAreIdentical(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)

	wrong := f.issue(t, testGroup, 1, "a@example.org")
	expired := f.issue(t, testGroup, 2, "b@example.org")
	exhausted := f.issue(t, testGroup, 3, "c@example.org")
	used := f.issue(t, testGroup, 4, "d@example.org")

	for i := 0; i < MaxAttempts; i++ {
		_, _ = f.svc.Verify(ctx, testGroup, 3, wrongCode(exhausted.Code))
	}
	if _, err := f.svc.Verify(ctx, testGroup, 4, used.Code); err != nil {
		t.Fatal(err)
	}

	cases := map[string]func() error{
		"faux": func() error {
			_, err := f.svc.Verify(ctx, testGroup, 1, wrongCode(wrong.Code))
			return err
		},
		"épuisé (bon code)": func() error {
			_, err := f.svc.Verify(ctx, testGroup, 3, exhausted.Code)
			return err
		},
		"déjà utilisé": func() error {
			_, err := f.svc.Verify(ctx, testGroup, 4, used.Code)
			return err
		},
		"inscription inconnue": func() error {
			_, err := f.svc.Verify(ctx, testGroup, 99, wrong.Code)
			return err
		},
		"mal formé": func() error {
			_, err := f.svc.Verify(ctx, testGroup, 1, "0O1IL!")
			return err
		},
		"vide": func() error {
			_, err := f.svc.Verify(ctx, testGroup, 1, "")
			return err
		},
	}
	got := map[string]*errs.Error{}
	for name, run := range cases {
		got[name] = refusalOf(t, run())
	}
	f.clk.Advance(TTL)
	_, err := f.svc.Verify(ctx, testGroup, 2, expired.Code)
	got["expiré (bon code)"] = refusalOf(t, err)

	var ref *errs.Error
	for name, e := range got {
		if !IsRefused(e) || e.Kind != errs.KindUnauthorized {
			t.Errorf("%s : %v (%s)", name, e, e.Kind)
		}
		if ref == nil {
			ref = e
			continue
		}
		if !reflect.DeepEqual(e, ref) {
			t.Errorf("%s : réponse %#v différente de %#v", name, e, ref)
		}
	}
	// Le message ne nomme aucune cause.
	for _, word := range []string{"expiré", "tentative", "épuisé", "inconnu", "utilisé"} {
		if strings.Contains(strings.ToLower(ref.Message), word) {
			t.Errorf("le message %q révèle la cause (%q)", ref.Message, word)
		}
	}
}

func TestConfirmLinkRefusalsAreIdentical(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	used := f.issue(t, testGroup, 1, "a@example.org")
	expired := f.issue(t, testGroup, 2, "b@example.org")
	exhausted := f.issue(t, testGroup, 3, "c@example.org")
	other := f.issue(t, testGroup+1, 1, "d@example.org")

	if _, err := f.svc.Confirm(ctx, testGroup, tokenOf(t, used)); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < MaxAttempts; i++ {
		_, _ = f.svc.Verify(ctx, testGroup, 3, wrongCode(exhausted.Code))
	}

	h := ConfirmHandler{
		Service: f.svc,
		Group:   func(*http.Request) (int64, bool) { return testGroup, true },
		Page:    func(w http.ResponseWriter, _ *http.Request, _ ConfirmPageData) { w.WriteHeader(200) },
		Done:    func(w http.ResponseWriter, _ *http.Request, _ Result) { w.WriteHeader(200) },
	}
	mux := http.NewServeMux()
	mux.Handle("GET /c/{token}", h.PageHandler())
	mux.Handle("POST /c/{token}", h.SubmitHandler())

	toks := map[string]string{
		"déjà utilisé":   tokenOf(t, used),
		"épuisé":         tokenOf(t, exhausted),
		"autre groupe":   tokenOf(t, other),
		"inconnu":        "AAAAAAAAAAAAAAAAAAAAAA",
		"mal formé":      "x",
		"expiré (après)": tokenOf(t, expired),
	}
	type resp struct {
		code int
		body string
	}
	var ref *resp
	for _, method := range []string{http.MethodGet, http.MethodPost} {
		for name, tok := range toks {
			if name == "expiré (après)" {
				continue
			}
			rec := httptest.NewRecorder()
			mux.ServeHTTP(rec, httptest.NewRequest(method, "/c/"+tok, nil))
			r := &resp{rec.Code, rec.Body.String()}
			if ref == nil {
				ref = r
			}
			if *r != *ref {
				t.Errorf("%s %s : %+v différent de %+v", method, name, *r, *ref)
			}
		}
	}
	f.clk.Advance(TTL + time.Second)
	for _, method := range []string{http.MethodGet, http.MethodPost} {
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, httptest.NewRequest(method, "/c/"+toks["expiré (après)"], nil))
		if r := (resp{rec.Code, rec.Body.String()}); r != *ref {
			t.Errorf("%s expiré : %+v différent de %+v", method, r, *ref)
		}
	}
	if ref.code == http.StatusOK {
		t.Fatal("un refus répond 200")
	}
}
