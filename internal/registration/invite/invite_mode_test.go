package invite

import (
	"bytes"
	"encoding/base64"
	"errors"
	"html"
	"net/http"
	"strings"
	"testing"

	"secretsanta/internal/participants"
	"secretsanta/internal/shared/errs"
)

// F11.AC1 : désactivé par défaut ; l'activation donne /<slug>/rejoindre/<jeton>
// avec un jeton de 128 bits stocké haché ; la désactivation ferme aussitôt.

func TestModeDisabledByDefault(t *testing.T) {
	e := newEnv(t)
	st, err := e.svc.Settings(t.Context(), admin(e.g1), e.g1)
	if err != nil {
		t.Fatal(err)
	}
	if st.Enabled || st.HasToken {
		t.Fatalf("mode actif par défaut : %+v", st)
	}
	inv, _ := e.store.Invitation(t.Context(), e.g1)
	if inv.Enabled || inv.TokenHash != nil {
		t.Fatalf("invitation par défaut : %+v", inv)
	}
	for _, tok := range []string{"x", "AAAAAAAAAAAAAAAAAAAAAA", "noel"} {
		if _, err := e.svc.Open(t.Context(), "noel", tok); !errors.Is(err, ErrNeutral) {
			t.Fatalf("jeton %q accepté sans activation : %v", tok, err)
		}
	}
	if rec := get(e.server(t), "/noel/rejoindre/AAAAAAAAAAAAAAAAAAAAAA"); rec.Code != http.StatusNotFound {
		t.Fatalf("réponse neutre attendue : %d", rec.Code)
	}
}

func TestEnableGeneratesHashedTokenLink(t *testing.T) {
	e := newEnv(t)
	l := e.enable(t, e.g1)
	if l.Path != "/noel/rejoindre/"+l.Token {
		t.Fatalf("lien : %q", l.Path)
	}
	raw, err := base64.RawURLEncoding.DecodeString(l.Token)
	if err != nil || len(raw) != 16 {
		t.Fatalf("jeton de 128 bits attendu : %q (%d octets, %v)", l.Token, len(raw), err)
	}
	if strings.Contains(l.Token, "noel") {
		t.Fatal("le jeton contient le slug")
	}
	inv, _ := e.store.Invitation(t.Context(), e.g1)
	if !inv.Enabled || len(inv.TokenHash) != 32 {
		t.Fatalf("invitation : %+v", inv)
	}
	if bytes.Contains(inv.TokenHash, []byte(l.Token)) || bytes.Contains(inv.TokenHash, raw) {
		t.Fatal("jeton stocké en clair")
	}
	// Savoir le slug ne suffit pas : seul le jeton ouvre le formulaire.
	for _, tok := range []string{"noel", "rejoindre", strings.ToUpper(l.Token)} {
		if _, err := e.svc.Open(t.Context(), "noel", tok); !errors.Is(err, ErrNeutral) {
			t.Fatalf("jeton %q accepté", tok)
		}
	}
	inv2, err := e.svc.Open(t.Context(), "noel", l.Token)
	if err != nil || inv2.GroupID != e.g1 || inv2.Closed != "" {
		t.Fatalf("ouverture : %+v %v", inv2, err)
	}
	rec := get(e.server(t), l.Path)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `name="csrf_token"`) {
		t.Fatalf("formulaire attendu : %d %s", rec.Code, rec.Body.String())
	}
	// Deux activations donnent deux jetons différents.
	if l2 := e.enable(t, e.g1); l2.Token == l.Token {
		t.Fatal("jeton répété")
	}
}

func TestDisableClosesFormImmediately(t *testing.T) {
	e := newEnv(t)
	l := e.enable(t, e.g1)
	h := e.server(t)
	page := get(h, l.Path)
	if page.Code != http.StatusOK {
		t.Fatalf("formulaire : %d", page.Code)
	}
	if err := e.svc.Disable(t.Context(), admin(e.g1), e.g1); err != nil {
		t.Fatal(err)
	}
	if rec := get(h, l.Path); rec.Code != http.StatusNotFound {
		t.Fatalf("formulaire encore ouvert : %d", rec.Code)
	}
	if _, err := e.svc.Submit(t.Context(), "noel", l.Token, sub("Anne", "Lebrun", "anne@example.org")); !errors.Is(err, ErrNeutral) {
		t.Fatalf("soumission acceptée après désactivation : %v", err)
	}
	// Un formulaire ouvert avant la désactivation ne peut plus être soumis.
	m := csrfRe.FindStringSubmatch(page.Body.String())
	form := formOf("Anne", "Lebrun", "anne@example.org")
	form.Set("csrf_token", html.UnescapeString(m[1]))
	if rec := rawPost(h, l.Path, form, page.Result().Cookies(), true); rec.Code != http.StatusNotFound {
		t.Fatalf("POST après désactivation : %d", rec.Code)
	}
	if e.pendingCount(t, e.g1) != 0 || e.codes.count() != 0 {
		t.Fatal("inscription créée après désactivation")
	}
	st, _ := e.svc.Settings(t.Context(), admin(e.g1), e.g1)
	if st.Enabled || st.HasToken {
		t.Fatalf("état après désactivation : %+v", st)
	}
	// Réactiver émet un nouveau jeton ; l'ancien reste neutre.
	l2 := e.enable(t, e.g1)
	if _, err := e.svc.Open(t.Context(), "noel", l.Token); !errors.Is(err, ErrNeutral) {
		t.Fatal("ancien jeton revenu à la réactivation")
	}
	if _, err := e.svc.Open(t.Context(), "noel", l2.Token); err != nil {
		t.Fatal(err)
	}
}

func TestOnlyTheAdminControlsTheMode(t *testing.T) {
	e := newEnv(t)
	for _, a := range []participants.Actor{{}, participants.ParticipantActor(e.g1, 1)} {
		if _, err := e.svc.Enable(t.Context(), a, e.g1); err == nil {
			t.Fatalf("activation par %+v", a)
		}
		if err := e.svc.Disable(t.Context(), a, e.g1); err == nil {
			t.Fatalf("désactivation par %+v", a)
		}
		if _, err := e.svc.Settings(t.Context(), a, e.g1); err == nil {
			t.Fatalf("lecture par %+v", a)
		}
	}
	if _, err := e.svc.Enable(t.Context(), participants.Actor{}, e.g1); !errs.Is(err, errs.KindUnauthorized) {
		t.Fatalf("anonyme : %v", err)
	}
}
