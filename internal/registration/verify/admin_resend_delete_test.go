package verify

import (
	"errors"
	"testing"
	"time"

	"secretsanta/internal/registration/code"
	"secretsanta/internal/shared/errs"
)

// F11.AC18 : « Renvoyer le code » respecte le délai de 60 secondes et
// remplace l'ancien code.
func TestAdminResendRespectsDelay(t *testing.T) {
	e := newEnv(t)
	ctx := t.Context()
	r := e.register(t, e.g1, "Léa", "Martin", "lea@example.org")
	first := e.code(t, r)
	firstTok := e.confirmToken(t, r)

	e.clock.Advance(59 * time.Second)
	_, err := e.svc.ResendCode(ctx, admin(e.g1), e.g1, r.id)
	if !errors.Is(err, code.ErrTooSoon) {
		t.Fatalf("renvoi à 59 s : %v", err)
	}
	if e.codeMail.count() != 1 {
		t.Fatal("un code est parti avant 60 secondes")
	}

	e.clock.Advance(time.Second)
	issued, err := e.svc.ResendCode(ctx, admin(e.g1), e.g1, r.id)
	if err != nil {
		t.Fatalf("renvoi à 60 s : %v", err)
	}
	if !issued.ResendAt.Equal(e.clock.Now().Add(code.ResendDelay)) {
		t.Fatalf("prochain renvoi %v", issued.ResendAt)
	}
	m := e.codeMail.last(t, e.g1, r.id)
	if e.codeMail.count() != 2 || m.To != "lea@example.org" || m.GroupName != "Groupe noel" {
		t.Fatalf("courriel renvoyé %+v", m)
	}
	// Juste après : de nouveau trop tôt.
	if _, err := e.svc.ResendCode(ctx, admin(e.g1), e.g1, r.id); !errors.Is(err, code.ErrTooSoon) {
		t.Fatalf("second renvoi immédiat : %v", err)
	}
	// L'ancien code et l'ancien lien ne servent plus ; le nouveau oui.
	if first != m.Code {
		_, err = e.svc.VerifyCode(ctx, e.g1, r.ref, first)
		mustRefused(t, err)
	}
	_, err = e.svc.PreviewLink(ctx, e.g1, firstTok)
	mustRefused(t, err)
	if _, err := e.svc.VerifyCode(ctx, e.g1, r.ref, m.Code); err != nil {
		t.Fatalf("nouveau code : %v", err)
	}

	// Journalisé ; réservé à l'administrateur du groupe.
	log, _ := e.svc.Audit(ctx, admin(e.g1), e.g1)
	if len(log) != 1 || log[0].Action != ActionResend || log[0].RegistrationID != r.id {
		t.Fatalf("journal %+v", log)
	}
	r2 := e.register(t, e.g1, "Noé", "Bernard", "noe@example.org")
	e.clock.Advance(time.Minute)
	if _, err := e.svc.ResendCode(ctx, admin(e.g2), e.g1, r2.id); !errs.Is(err, errs.KindForbidden) {
		t.Fatalf("renvoi par un autre administrateur : %v", err)
	}
	if _, err := e.svc.ResendCode(ctx, admin(e.g2), e.g2, r2.id); !errs.Is(err, errs.KindNotFound) {
		t.Fatalf("renvoi d'une inscription d'un autre groupe : %v", err)
	}
}

// F11.AC18 : « Supprimer » retire l'inscription et invalide son code et son
// lien de confirmation.
func TestAdminDeleteRemovesRegistrationAndCode(t *testing.T) {
	e := newEnv(t)
	ctx := t.Context()
	r := e.register(t, e.g1, "Léa", "Martin", "lea@example.org")
	keep := e.register(t, e.g1, "Noé", "Bernard", "noe@example.org")
	c, tok := e.code(t, r), e.confirmToken(t, r)

	if err := e.svc.DeleteRegistration(ctx, admin(e.g2), e.g2, r.id); !errs.Is(err, errs.KindNotFound) {
		t.Fatalf("suppression depuis un autre groupe : %v", err)
	}
	if err := e.svc.DeleteRegistration(ctx, admin(e.g1), e.g1, r.id); err != nil {
		t.Fatal(err)
	}
	views, err := e.svc.ListPending(ctx, admin(e.g1), e.g1)
	if err != nil {
		t.Fatal(err)
	}
	if len(views) != 1 || views[0].ID != keep.id {
		t.Fatalf("inscriptions restantes %+v", views)
	}
	all, _ := e.pending.ListPending(ctx, e.g1, time.Time{})
	if len(all) != 1 {
		t.Fatalf("%d inscriptions stockées", len(all))
	}
	_, err = e.svc.VerifyCode(ctx, e.g1, r.ref, c)
	mustRefused(t, err)
	_, err = e.svc.PreviewLink(ctx, e.g1, tok)
	mustRefused(t, err)
	_, err = e.svc.ConfirmLink(ctx, e.g1, tok)
	mustRefused(t, err)
	// Même au niveau du code de F11.3 : rien ne reste.
	_, err = e.codes.Verify(ctx, e.g1, r.id, c)
	mustRefused(t, err)
	if len(e.participants(t, e.g1)) != 0 {
		t.Fatal("participant créé")
	}
	log, _ := e.svc.Audit(ctx, admin(e.g1), e.g1)
	if len(log) != 1 || log[0].Action != ActionDelete || log[0].RegistrationID != r.id {
		t.Fatalf("journal %+v", log)
	}
	if err := e.svc.DeleteRegistration(ctx, admin(e.g1), e.g1, r.id); !errs.Is(err, errs.KindNotFound) {
		t.Fatalf("seconde suppression : %v", err)
	}
	// L'autre inscription fonctionne toujours.
	if _, err := e.svc.VerifyCode(ctx, e.g1, keep.ref, e.code(t, keep)); err != nil {
		t.Fatal(err)
	}
}
