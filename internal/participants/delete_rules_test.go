package participants

import (
	"strings"
	"testing"

	"secretsanta/internal/shared/errs"
)

func TestDeleteNeedsConfirmation(t *testing.T) {
	e := newEnv(t)
	p := e.add(t, "A", "B")
	wantKind(t, e.svc.Delete(t.Context(), e.admin(), e.g1, p.ID, false), errs.KindValidation)
	if _, err := e.svc.Get(t.Context(), e.admin(), e.g1, p.ID); err != nil {
		t.Fatal("le participant ne doit pas être supprimé sans confirmation")
	}
	if err := e.svc.Delete(t.Context(), e.admin(), e.g1, p.ID, true); err != nil {
		t.Fatal(err)
	}
	_, err := e.svc.Get(t.Context(), e.admin(), e.g1, p.ID)
	wantKind(t, err, errs.KindNotFound)
}

func TestDeleteRefusedAfterDraw(t *testing.T) {
	e := newEnv(t)
	p := e.add(t, "A", "B")
	e.draw.Done[e.g1] = true
	err := e.svc.Delete(t.Context(), e.admin(), e.g1, p.ID, true)
	wantKind(t, err, errs.KindConflict)
	if !strings.Contains(err.Error(), "Ajuster le tirage") {
		t.Fatalf("renvoi vers l'ajustement attendu : %v", err)
	}
	if _, err := e.svc.Get(t.Context(), e.admin(), e.g1, p.ID); err != nil {
		t.Fatal("le participant doit rester")
	}
}

func TestDeleteNeedsAdminOfGroup(t *testing.T) {
	e := newEnv(t)
	p := e.add(t, "A", "B")
	wantKind(t, e.svc.Delete(t.Context(), Actor{}, e.g1, p.ID, true), errs.KindUnauthorized)
	wantKind(t, e.svc.Delete(t.Context(), AdminActor(e.g2), e.g1, p.ID, true), errs.KindForbidden)
	wantKind(t, e.svc.Delete(t.Context(), ParticipantActor(e.g1, p.ID), e.g1, p.ID, true), errs.KindForbidden)
}
