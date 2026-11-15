package mailunsub

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

func off(t *testing.T, e *env, typ string) bool {
	t.Helper()
	v, err := e.svc.IsUnsubscribed(context.Background(), pid, typ)
	if err != nil {
		t.Fatal(err)
	}
	return v
}

// F6.AC17 : le POST signé coupe le type sans session ; faux jeton ou autre type
// refusé ; la page permet de se réabonner ; IsUnsubscribed reflète l'état.
func TestSignedPostUnsubscribesWithoutSession(t *testing.T) {
	e := newEnv(t)
	path := e.link(t, TypeRelances)

	// Le GET ne change rien.
	rec := e.do(http.MethodGet, path, nil, "fr")
	if rec.Code != 200 || off(t, e, TypeRelances) {
		t.Fatalf("GET : %d, désinscrit=%v", rec.Code, off(t, e, TypeRelances))
	}
	// POST un clic : sans cookie, sans jeton CSRF, origine étrangère.
	rec = e.do(http.MethodPost, path, oneClick, "fr")
	if rec.Code != 200 {
		t.Fatalf("POST un clic : %d %s", rec.Code, rec.Body.String())
	}
	if !off(t, e, TypeRelances) {
		t.Fatal("relances doit être coupé")
	}
	if off(t, e, TypeNouvelleIdee) {
		t.Fatal("nouvelle_idee ne doit pas bouger")
	}
	// Une seconde fois : idempotent.
	if rec = e.do(http.MethodPost, path, oneClick, "fr"); rec.Code != 200 || !off(t, e, TypeRelances) {
		t.Fatal("POST répété")
	}
	// Le GET montre l'état coupé et le bouton de réabonnement.
	rec = e.do(http.MethodGet, path, nil, "fr")
	if !strings.Contains(rec.Body.String(), `value="resubscribe"`) {
		t.Fatal("bouton de réabonnement absent")
	}
	// Réabonnement par la page.
	rec = e.do(http.MethodPost, path, url.Values{"action": {"resubscribe"}}, "fr")
	if rec.Code != 200 || off(t, e, TypeRelances) {
		t.Fatalf("réabonnement : %d, coupé=%v", rec.Code, off(t, e, TypeRelances))
	}
}

func TestForgedOrSwappedTokenIsRefused(t *testing.T) {
	e := newEnv(t)
	path := e.link(t, TypeRelances)
	tok := strings.TrimPrefix(path, RoutePrefix)
	payload, sig, _ := strings.Cut(tok, ".")
	other, _ := e.svc.Signer().Sign(Claims{GroupID: e.group.ID, ParticipantID: pid, Type: TypeNouvelleIdee})
	otherPayload, _, _ := strings.Cut(other, ".")
	cases := map[string]string{
		"signature altérée":         RoutePrefix + payload + "." + sig[:len(sig)-2] + "AA",
		"autre type même signature": RoutePrefix + otherPayload + "." + sig,
		"sans signature":            RoutePrefix + payload,
		"n'importe quoi":            RoutePrefix + "abc.def",
		"autre secret":              "",
	}
	forged, _ := NewSigner([]byte("autre-secret")).Sign(Claims{GroupID: e.group.ID, ParticipantID: pid, Type: TypeRelances})
	cases["autre secret"] = RoutePrefix + forged
	for name, p := range cases {
		for _, m := range []string{http.MethodGet, http.MethodPost} {
			if rec := e.do(m, p, oneClick, "fr"); rec.Code != http.StatusNotFound {
				t.Errorf("%s %s : 404 attendu, %d", m, name, rec.Code)
			}
		}
	}
	if off(t, e, TypeRelances) || off(t, e, TypeNouvelleIdee) {
		t.Fatal("un jeton refusé a modifié l'état")
	}
}

func TestTypesAreIndependentAndUnknownIsNeverOff(t *testing.T) {
	e := newEnv(t)
	e.do(http.MethodPost, e.link(t, TypeNouvelleIdee), oneClick, "fr")
	if !off(t, e, TypeNouvelleIdee) || off(t, e, TypeRelances) {
		t.Fatal("types dépendants")
	}
	if off(t, e, "lien_magique") {
		t.Fatal("un type essentiel n'est jamais coupé")
	}
	if err := e.svc.Unsubscribe(context.Background(), 1, pid, "lien_magique"); err == nil {
		t.Fatal("couper un type essentiel doit échouer")
	}
}
