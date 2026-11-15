package public

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"testing"

	"secretsanta/internal/access/limiter"
	"secretsanta/internal/i18n/negotiate"
	"secretsanta/internal/registration/invite"
	"secretsanta/internal/registration/verify"
	"secretsanta/internal/shared/errs"
	"secretsanta/internal/websec"
)

// zzInvites fait échouer Submit quand err est posé.
type zzInvites struct {
	*fakeInvites
	err error
}

func (z *zzInvites) Submit(ctx context.Context, slug, tok string, sub invite.Submission) (*invite.Result, error) {
	if z.err != nil {
		return nil, z.err
	}
	return z.fakeInvites.Submit(ctx, slug, tok, sub)
}

// zzVerify fait échouer les trois opérations avec err.
type zzVerify struct {
	*fakeVerify
	err error
}

func (z *zzVerify) VerifyCode(ctx context.Context, g int64, ref, in string) (*verify.Success, error) {
	if z.err != nil {
		return nil, z.err
	}
	return z.fakeVerify.VerifyCode(ctx, g, ref, in)
}

func (z *zzVerify) ConfirmLink(ctx context.Context, g int64, tok string) (*verify.Success, error) {
	if z.err != nil {
		return nil, z.err
	}
	return z.fakeVerify.ConfirmLink(ctx, g, tok)
}

// zzEnv reconstruit les pages avec des doublures capables d'échouer.
func zzEnv(t *testing.T) (*env, *zzInvites, *zzVerify) {
	t.Helper()
	e := newEnv(t)
	inv := &zzInvites{fakeInvites: e.invites}
	ver := &zzVerify{fakeVerify: e.verify}
	pages, err := New(Options{Groups: &fakeGroupsRef{e}, Invites: inv, Verify: ver, Translator: e.cat,
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	pages.Register(mux)
	pol, err := websec.NewPolicy(publicURL)
	if err != nil {
		t.Fatal(err)
	}
	csrf := websec.NewCSRF(pol, websec.CSRFOptions{}).Middleware(mux)
	e.handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		csrf.ServeHTTP(w, r.WithContext(negotiate.WithLang(r.Context(), "fr")))
	})
	return e, inv, ver
}

func TestZZSubmitErrors(t *testing.T) {
	e, inv, _ := zzEnv(t)
	form := url.Values{"prenom": {"Camille"}}
	for name, c := range map[string]struct {
		err  error
		code int
	}{
		"neutre":  {invite.ErrNeutral, http.StatusNotFound},
		"limite":  {errors.Join(errs.Forbidden("trop"), limiter.ErrLimited), http.StatusTooManyRequests},
		"interne": {errors.New("panne"), http.StatusInternalServerError},
	} {
		inv.err = c.err
		r := e.do(t, http.MethodPost, joinPath, form, "fr")
		if name == "neutre" {
			if r.Code == http.StatusOK {
				t.Errorf("%s : %d", name, r.Code)
			}
			continue
		}
		if r.Code != c.code {
			t.Errorf("%s : %d, %d attendu", name, r.Code, c.code)
		}
	}
	inv.err = nil
	// Méthode inattendue sur le formulaire.
	if r := e.do(t, http.MethodPut, joinPath, nil, "fr"); r.Code == http.StatusOK {
		t.Errorf("PUT : %d", r.Code)
	}
	// Issues invalides : statut 422.
	e.invites.result = &invite.Result{Outcome: invite.OutcomeInvalid}
	if r := e.do(t, http.MethodPost, joinPath, form, "fr"); r.Code != http.StatusUnprocessableEntity {
		t.Errorf("invalide : %d", r.Code)
	}
}

func TestZZConfirmAndCodeFailures(t *testing.T) {
	e, _, ver := zzEnv(t)
	confirm := "/noel/inscription/confirmer/lien"
	code := "/noel/inscription/code"

	if r := e.do(t, http.MethodGet, confirm, nil, "fr"); r.Code != 200 {
		t.Fatalf("page de confirmation : %d", r.Code)
	}
	if r := e.do(t, http.MethodGet, "/noel/inscription/confirmer/faux", nil, "fr"); r.Code != http.StatusNotFound {
		t.Errorf("jeton faux : %d", r.Code)
	}
	if r := e.do(t, http.MethodGet, "/inconnu/inscription/confirmer/lien", nil, "fr"); r.Code != http.StatusNotFound {
		t.Errorf("groupe inconnu : %d", r.Code)
	}
	if r := e.do(t, http.MethodPost, "/inconnu/inscription/confirmer/lien", nil, "fr"); r.Code != http.StatusNotFound {
		t.Errorf("POST groupe inconnu : %d", r.Code)
	}
	if r := e.do(t, http.MethodPost, "/inconnu/inscription/code", codeForm("ref-1", "ABC123"), "fr"); r.Code != http.StatusNotFound {
		t.Errorf("code, groupe inconnu : %d", r.Code)
	}
	if r := e.do(t, http.MethodPost, confirm, nil, "fr"); r.Code != 200 {
		t.Errorf("confirmation : %d", r.Code)
	}
	if r := e.do(t, http.MethodPost, code, codeForm("ref-1", "ABC123"), "fr"); r.Code != 200 {
		t.Errorf("code : %d", r.Code)
	}

	// VerificationAllowed refuse : fermeture, plafond, refus, panne.
	for name, c := range map[string]struct {
		err  error
		code int
	}{
		"fermeture": {errs.Forbidden("x").WithCode("registration.closed_draw"), http.StatusForbidden},
		"inconnue":  {errs.Forbidden("x").WithCode("autre"), http.StatusForbidden},
		"limite":    {errors.Join(errs.Forbidden("x"), limiter.ErrLimited), http.StatusTooManyRequests},
		"absent":    {errs.NotFound("x"), http.StatusNotFound},
		"panne":     {errors.New("panne"), http.StatusInternalServerError},
	} {
		e.invites.verifyErr = c.err
		for _, tc := range []struct{ method, path string }{{http.MethodGet, confirm}, {http.MethodPost, confirm}, {http.MethodPost, code}} {
			var f url.Values
			if tc.path == code {
				f = codeForm("ref-1", "ABC123")
			}
			if r := e.do(t, tc.method, tc.path, f, "fr"); r.Code != c.code {
				t.Errorf("%s %s %s : %d, %d attendu", name, tc.method, tc.path, r.Code, c.code)
			}
		}
	}
	e.invites.verifyErr = nil

	// Le service de vérification échoue.
	ver.err = errors.New("panne")
	for _, tc := range []struct{ method, path string }{{http.MethodPost, confirm}, {http.MethodPost, code}} {
		var f url.Values
		if tc.path == code {
			f = codeForm("ref-1", "ABC123")
		}
		if r := e.do(t, tc.method, tc.path, f, "fr"); r.Code != http.StatusInternalServerError {
			t.Errorf("panne %s : %d", tc.path, r.Code)
		}
	}
}
