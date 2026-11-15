package verify

import (
	"testing"
	"time"

	"secretsanta/internal/shared/errs"
	"secretsanta/internal/shared/ports"
)

// F11.AC19 : RegistrationState compte les inscriptions en attente ;
// « ignorer » les garde côté administration mais fermées, code invalidé, et
// ne les compte plus ; « supprimer » les purge.
func TestRegistrationStatePortIgnore(t *testing.T) {
	e := newEnv(t)
	ctx := t.Context()
	var port ports.RegistrationState = e.svc
	a := e.register(t, e.g1, "Léa", "Martin", "lea@example.org")
	b := e.register(t, e.g1, "Noé", "Bernard", "noe@example.org")
	other := e.register(t, e.g2, "Inès", "Petit", "ines@example.org")

	if n, err := port.PendingCount(ctx, e.g1); err != nil || n != 2 {
		t.Fatalf("PendingCount = %d, %v", n, err)
	}
	if err := port.Resolve(ctx, e.g1, ports.ResolveIgnore); err != nil {
		t.Fatal(err)
	}
	if n, _ := port.PendingCount(ctx, e.g1); n != 0 {
		t.Fatalf("PendingCount après « ignorer » = %d", n)
	}
	// Conservées côté administration, marquées ignorées.
	views, err := e.svc.ListPending(ctx, admin(e.g1), e.g1)
	if err != nil {
		t.Fatal(err)
	}
	if len(views) != 2 || !views[0].Ignored || !views[1].Ignored {
		t.Fatalf("vue d'administration %+v", views)
	}
	// Fermées : codes et liens invalidés, pas de renvoi, pas de validation.
	for _, r := range []registration{a, b} {
		_, err := e.svc.VerifyCode(ctx, e.g1, r.ref, e.code(t, r))
		mustRefused(t, err)
		_, err = e.svc.ConfirmLink(ctx, e.g1, e.confirmToken(t, r))
		mustRefused(t, err)
		_, err = e.codes.Verify(ctx, e.g1, r.id, e.code(t, r))
		mustRefused(t, err)
	}
	e.clock.Advance(2 * time.Minute)
	if _, err := e.svc.ResendCode(ctx, admin(e.g1), e.g1, a.id); !errs.Is(err, errs.KindForbidden) {
		t.Fatalf("renvoi d'une inscription ignorée : %v", err)
	}
	if _, err := e.svc.ValidateWithoutCode(ctx, admin(e.g1), e.g1, a.id); !errs.Is(err, errs.KindForbidden) {
		t.Fatalf("validation d'une inscription ignorée : %v", err)
	}
	if len(e.participants(t, e.g1)) != 0 {
		t.Fatal("une inscription ignorée est devenue participante")
	}
	// L'autre groupe n'est pas touché.
	if n, _ := port.PendingCount(ctx, e.g2); n != 1 {
		t.Fatalf("autre groupe : %d", n)
	}
	if _, err := e.svc.VerifyCode(ctx, e.g2, other.ref, e.code(t, other)); err != nil {
		t.Fatalf("autre groupe : %v", err)
	}
	// Une nouvelle inscription après « ignorer » est de nouveau comptée.
	e.register(t, e.g1, "Jade", "Roux", "jade@example.org")
	if n, _ := port.PendingCount(ctx, e.g1); n != 1 {
		t.Fatalf("nouvelle inscription : %d", n)
	}
}

func TestRegistrationStatePortDelete(t *testing.T) {
	e := newEnv(t)
	ctx := t.Context()
	var port ports.RegistrationState = e.svc
	a := e.register(t, e.g1, "Léa", "Martin", "lea@example.org")
	b := e.register(t, e.g1, "Noé", "Bernard", "noe@example.org")
	e.register(t, e.g2, "Inès", "Petit", "ines@example.org")
	// Une inscription déjà ignorée est purgée aussi.
	if err := e.store.SetIgnored(ctx, e.g1, b.id, e.clock.Now()); err != nil {
		t.Fatal(err)
	}

	if err := port.Resolve(ctx, e.g1, ports.ResolveDelete); err != nil {
		t.Fatal(err)
	}
	if n, _ := port.PendingCount(ctx, e.g1); n != 0 {
		t.Fatalf("PendingCount après « supprimer » = %d", n)
	}
	all, _ := e.pending.ListPending(ctx, e.g1, time.Time{})
	if len(all) != 0 {
		t.Fatalf("%d inscriptions non purgées", len(all))
	}
	if ig, _ := e.store.Ignored(ctx, e.g1); len(ig) != 0 {
		t.Fatalf("états ignorés restants %v", ig)
	}
	for _, r := range []registration{a, b} {
		_, err := e.codes.Verify(ctx, e.g1, r.id, e.code(t, r))
		mustRefused(t, err)
	}
	if n, _ := port.PendingCount(ctx, e.g2); n != 1 {
		t.Fatalf("autre groupe purgé : %d", n)
	}
	if err := port.Resolve(ctx, e.g1, "oublier"); !errs.Is(err, errs.KindValidation) {
		t.Fatalf("choix inconnu : %v", err)
	}
}
