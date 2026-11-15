package mailunsub

import (
	"bytes"
	"context"
	"net/mail"
	"strings"
	"testing"

	"secretsanta/internal/mail/templates"
)

func assertEssentialMail(t *testing.T, e *env, k templates.Kind) {
	t.Helper()
	info, _ := templates.InfoOf(k)
	if !info.Essential {
		t.Fatalf("%s doit être essentiel", k)
	}
	d := sample(k)
	// Même si l'appelant avait posé un lien, il est retiré.
	d.UnsubscribeURL = publicURL + RoutePrefix + "x.y"
	m, raw := render(t, e.cat, e.svc, k, d)
	if m.UnsubscribeURL != "" || strings.Contains(m.Text, RoutePrefix) || strings.Contains(m.HTML, RoutePrefix) {
		t.Errorf("%s : lien de désinscription présent", k)
	}
	msg, err := mail.ReadMessage(bytes.NewReader([]byte(raw)))
	if err != nil {
		t.Fatal(err)
	}
	for _, h := range []string{"List-Unsubscribe", "List-Unsubscribe-Post"} {
		if msg.Header.Get(h) != "" {
			t.Errorf("%s : en-tête %s présent", k, h)
		}
	}
}

// F6.AC18 : lien d'accès, invitation, lien magique, tirage prêt, ajustement
// (et budget mis à jour) sans lien de désinscription, jamais désactivables.
func TestEssentialMailsHaveNoUnsubscribeAndCannotBeDisabled(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	kinds := []templates.Kind{templates.Invitation, templates.MagicLink, templates.ReadyOne, templates.ReadyTwo,
		templates.ReadyReception, templates.DrawAdjusted, templates.BudgetUpdated}
	for _, k := range kinds {
		assertEssentialMail(t, e, k)
	}
	// Désinscrit des deux types : les essentiels partent quand même.
	for _, typ := range []string{TypeRelances, TypeNouvelleIdee} {
		if err := e.svc.Unsubscribe(ctx, e.group.ID, pid, typ); err != nil {
			t.Fatal(err)
		}
	}
	for _, k := range kinds {
		if ok, err := e.svc.ShouldSend(ctx, k, pid); err != nil || !ok {
			t.Errorf("%s doit partir malgré les désinscriptions (%v)", k, err)
		}
	}
	// Les mails non essentiels, eux, sont coupés.
	for _, k := range []templates.Kind{templates.ReminderParticipant, templates.ReminderLetter, templates.ReminderReading, templates.LetterUpdated} {
		if ok, _ := e.svc.ShouldSend(ctx, k, pid); ok {
			t.Errorf("%s ne doit pas partir", k)
		}
	}
	// Aucun type essentiel ne peut être signé ni enregistré.
	for _, bad := range []string{"lien_magique", "invitation", "tirage_pret", "ajustement", "acces", ""} {
		if _, err := e.svc.URL(1, pid, bad); err == nil {
			t.Errorf("lien signé pour %q", bad)
		}
		if err := e.svc.Unsubscribe(ctx, 1, pid, bad); err == nil {
			t.Errorf("désinscription de %q acceptée", bad)
		}
	}
	// Tout modèle essentiel est exclu : aucun type d'un modèle essentiel n'est désactivable.
	for _, k := range templates.Kinds() {
		if info, _ := templates.InfoOf(k); info.Essential && ValidType(info.UnsubType) {
			t.Errorf("%s : essentiel mais avec un type désactivable", k)
		}
	}
}
