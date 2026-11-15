package participants

import (
	"strings"
	"testing"

	"secretsanta/internal/shared/errs"
)

func role(t *testing.T, e *env, id int64) DrawRole {
	t.Helper()
	p, err := e.svc.Get(t.Context(), e.admin(), e.g1, id)
	if err != nil {
		t.Fatal(err)
	}
	return p.Role
}

func TestAdminSetsAnyRoleAnytime(t *testing.T) {
	e := newEnv(t)
	p := e.add(t, "A", "B")
	for _, done := range []bool{false, true} {
		e.draw.Done[e.g1] = done
		for _, r := range []DrawRole{RoleReceptionSeule, RoleOffrePlus, RoleUnSeul, RoleNormal} {
			if err := e.svc.SetRole(t.Context(), e.admin(), e.g1, p.ID, r); err != nil {
				t.Fatalf("admin %s (tirage=%v) : %v", r, done, err)
			}
			if role(t, e, p.ID) != r {
				t.Fatalf("rôle non posé : %s", r)
			}
		}
	}
}

func TestPersonSetsOnlyNormalOrUnSeulBeforeDraw(t *testing.T) {
	e := newEnv(t)
	p := e.add(t, "A", "B")
	me := ParticipantActor(e.g1, p.ID)
	if err := e.svc.SetRole(t.Context(), me, e.g1, p.ID, RoleUnSeul); err != nil {
		t.Fatal(err)
	}
	if err := e.svc.SetRole(t.Context(), me, e.g1, p.ID, RoleNormal); err != nil {
		t.Fatal(err)
	}
	for _, r := range []DrawRole{RoleReceptionSeule, RoleOffrePlus, "autre"} {
		wantKind(t, e.svc.SetRole(t.Context(), me, e.g1, p.ID, r), errs.KindValidation)
	}
	if role(t, e, p.ID) != RoleNormal {
		t.Fatal("rôle modifié à tort")
	}
	// Pas celui d'un autre.
	q := e.add(t, "C", "D")
	wantKind(t, e.svc.SetRole(t.Context(), me, e.g1, q.ID, RoleUnSeul), errs.KindForbidden)
	// Anonyme et autre groupe.
	wantKind(t, e.svc.SetRole(t.Context(), Actor{}, e.g1, p.ID, RoleUnSeul), errs.KindUnauthorized)
	wantKind(t, e.svc.SetRole(t.Context(), ParticipantActor(e.g2, p.ID), e.g1, p.ID, RoleUnSeul), errs.KindForbidden)
}

func TestPersonLockedAfterDraw(t *testing.T) {
	e := newEnv(t)
	p := e.add(t, "A", "B")
	e.draw.Done[e.g1] = true
	err := e.svc.SetRole(t.Context(), ParticipantActor(e.g1, p.ID), e.g1, p.ID, RoleUnSeul)
	wantKind(t, err, errs.KindConflict)
	if !strings.Contains(err.Error(), "tirage") {
		t.Fatalf("explication attendue : %v", err)
	}
	if role(t, e, p.ID) != RoleNormal {
		t.Fatal("rôle modifié après tirage")
	}
}

func TestPersonCannotChangeAdminRole(t *testing.T) {
	e := newEnv(t)
	for _, r := range []DrawRole{RoleReceptionSeule, RoleOffrePlus} {
		p := e.add(t, "P"+string(r), "N")
		if err := e.svc.SetRole(t.Context(), e.admin(), e.g1, p.ID, r); err != nil {
			t.Fatal(err)
		}
		err := e.svc.SetRole(t.Context(), ParticipantActor(e.g1, p.ID), e.g1, p.ID, RoleNormal)
		wantKind(t, err, errs.KindForbidden)
		if role(t, e, p.ID) != r {
			t.Fatalf("%s modifié par la personne", r)
		}
	}
}

func TestRegisterNeverUnSeul(t *testing.T) {
	e := newEnv(t)
	_, err := e.svc.Register(t.Context(), e.g1, Input{FirstName: "A", Name: "B", Role: RoleUnSeul}, t0)
	wantKind(t, err, errs.KindValidation)
	for i, r := range []DrawRole{RoleNormal, RoleReceptionSeule, RoleOffrePlus} {
		if _, err := e.svc.Register(t.Context(), e.g1, Input{FirstName: "A", Name: string(rune('B' + i)), Role: r}, t0); err != nil {
			t.Fatal(err)
		}
	}
}
