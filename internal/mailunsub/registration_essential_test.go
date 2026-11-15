package mailunsub

import (
	"context"
	"testing"

	"secretsanta/internal/mail/templates"
)

// F6.AC19 : code de vérification et bienvenue sont essentiels : ils partent
// même après désinscription des deux types, sans lien de désinscription.
func TestVerificationAndWelcomeAreEssential(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	for _, typ := range []string{TypeRelances, TypeNouvelleIdee} {
		if err := e.svc.Unsubscribe(ctx, e.group.ID, pid, typ); err != nil {
			t.Fatal(err)
		}
	}
	for _, k := range []templates.Kind{templates.Verification, templates.Welcome} {
		assertEssentialMail(t, e, k)
		if ok, err := e.svc.ShouldSend(ctx, k, pid); err != nil || !ok {
			t.Errorf("%s doit partir : %v", k, err)
		}
	}
}
