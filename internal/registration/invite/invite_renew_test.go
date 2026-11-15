package invite

import (
	"crypto/subtle"
	"errors"
	"net/http"
	"testing"

	"secretsanta/internal/shared/errs"
)

// F11.AC2 : renouveler invalide l'ancien jeton (réponse neutre de F2) ; le
// jeton est comparé à temps constant.

// neutralShape renvoie ce qui doit être identique entre deux réponses neutres.
func neutralShape(t *testing.T, h http.Handler, path string) (int, string, http.Header) {
	t.Helper()
	rec := get(h, path)
	hdr := rec.Header().Clone()
	hdr.Del("Set-Cookie") // cookie CSRF aléatoire, posé sur toute réponse
	return rec.Code, rec.Body.String(), hdr
}

func TestRenewInvalidatesOldToken(t *testing.T) {
	e := newEnv(t)
	old := e.enable(t, e.g1)
	fresh, err := e.svc.Renew(t.Context(), admin(e.g1), e.g1)
	if err != nil {
		t.Fatal(err)
	}
	if fresh.Token == old.Token || fresh.Path != "/noel/rejoindre/"+fresh.Token {
		t.Fatalf("renouvellement : %+v", fresh)
	}
	if _, err := e.svc.Open(t.Context(), "noel", old.Token); !errors.Is(err, ErrNeutral) {
		t.Fatalf("ancien jeton accepté : %v", err)
	}
	if _, err := e.svc.Submit(t.Context(), "noel", old.Token, sub("Anne", "Lebrun", "anne@example.org")); !errors.Is(err, ErrNeutral) {
		t.Fatalf("soumission avec l'ancien jeton : %v", err)
	}
	if _, err := e.svc.Open(t.Context(), "noel", fresh.Token); err != nil {
		t.Fatalf("nouveau jeton refusé : %v", err)
	}

	// La réponse de l'ancien jeton est exactement la réponse neutre : slug
	// inconnu, jeton inventé, jeton absent, mode désactivé.
	h := e.server(t)
	code, body, hdr := neutralShape(t, h, old.Path)
	if code != http.StatusNotFound {
		t.Fatalf("statut : %d", code)
	}
	cases := []string{
		"/inconnu/rejoindre/" + fresh.Token,
		"/noel/rejoindre/AAAAAAAAAAAAAAAAAAAAAA",
		"/noel/rejoindre/",
		"/noel/rejoindre",
		"/famille/rejoindre/" + fresh.Token, // mode désactivé dans ce groupe
	}
	for _, p := range cases {
		c, b, hd := neutralShape(t, h, p)
		if c != code || b != body {
			t.Errorf("%s : réponse différente (%d)", p, c)
		}
		for k := range hdr {
			if hd.Get(k) != hdr.Get(k) {
				t.Errorf("%s : en-tête %s différent", p, k)
			}
		}
		if len(hd) != len(hdr) {
			t.Errorf("%s : en-têtes différents %v / %v", p, hd, hdr)
		}
	}
}

func TestRenewRequiresEnabledMode(t *testing.T) {
	e := newEnv(t)
	if _, err := e.svc.Renew(t.Context(), admin(e.g1), e.g1); !errs.Is(err, errs.KindValidation) {
		t.Fatalf("renouvellement sans activation : %v", err)
	}
	if _, err := e.svc.Renew(t.Context(), admin(e.g2), e.g1); !errs.Is(err, errs.KindForbidden) {
		t.Fatalf("renouvellement par un autre groupe : %v", err)
	}
}

func TestTokenComparedInConstantTime(t *testing.T) {
	e := newEnv(t)
	l := e.enable(t, e.g1)
	calls := 0
	var lens [][2]int
	constantTimeEq = func(a, b []byte) int {
		calls++
		lens = append(lens, [2]int{len(a), len(b)})
		return subtle.ConstantTimeCompare(a, b)
	}
	t.Cleanup(func() { constantTimeEq = subtle.ConstantTimeCompare })

	// Jeton valide, faux, de longueur différente, et groupe dont le mode est
	// désactivé : chaque fois une comparaison à temps constant, entre deux
	// condensés de même longueur (la durée ne dépend pas de la saisie).
	tries := []struct{ slug, tok string }{
		{"noel", l.Token},
		{"noel", "AAAAAAAAAAAAAAAAAAAAAA"},
		{"noel", l.Token + l.Token},
		{"noel", l.Token[:5]},
		{"famille", l.Token},
	}
	for i, tr := range tries {
		_, _ = e.svc.Open(t.Context(), tr.slug, tr.tok)
		if calls != i+1 {
			t.Fatalf("essai %d : %d comparaisons", i, calls)
		}
	}
	for _, n := range lens {
		if n[0] != 32 || n[1] != 32 {
			t.Fatalf("comparaison de longueurs %v", n)
		}
	}
	_, _ = e.svc.Submit(t.Context(), "noel", "faux", sub("A", "B", "a@example.org"))
	if calls != len(tries)+1 {
		t.Fatal("la soumission ne compare pas à temps constant")
	}
}
