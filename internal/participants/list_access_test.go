package participants

import (
	"testing"

	"secretsanta/internal/shared/errs"
)

func TestListAccess(t *testing.T) {
	e := newEnv(t)
	p := e.add(t, "A", "B")
	e.add(t, "C", "D")
	_, err := e.svc.List(t.Context(), Actor{}, e.g1)
	wantKind(t, err, errs.KindUnauthorized)
	_, err = e.svc.List(t.Context(), AdminActor(e.g2), e.g1)
	wantKind(t, err, errs.KindForbidden)
	_, err = e.svc.List(t.Context(), ParticipantActor(e.g2, p.ID), e.g1)
	wantKind(t, err, errs.KindForbidden)
	l, err := e.svc.List(t.Context(), e.admin(), e.g1)
	if err != nil || len(l) != 2 {
		t.Fatalf("admin : %v %v", l, err)
	}
	// Un participant voit seulement la vue publique.
	if _, err := e.svc.Create(t.Context(), e.admin(), e.g1, Input{FirstName: "E", Name: "F", Email: "e@f.fr", GiftHint: "secret"}); err != nil {
		t.Fatal(err)
	}
	l, err = e.svc.List(t.Context(), ParticipantActor(e.g1, p.ID), e.g1)
	if err != nil || len(l) != 3 {
		t.Fatalf("participant : %v %v", l, err)
	}
	for _, x := range l {
		if x.Email != "" || x.GiftHint != "" || x.Locale != "" {
			t.Fatalf("fuite dans la liste d'un participant : %+v", x)
		}
	}
	_, err = e.svc.Get(t.Context(), ParticipantActor(e.g1, p.ID), e.g1, l[2].ID)
	wantKind(t, err, errs.KindForbidden)
}
