package verify

import (
	"context"
	"testing"
	"time"

	"secretsanta/internal/enrolment"
	"secretsanta/internal/participants"
	"secretsanta/internal/registration/invite"
	"secretsanta/internal/shared/errs"
)

// recordingEnroller garde chaque entrée transmise à CreateVerified.
type recordingEnroller struct {
	next Enroller
	ins  []participants.Input
}

func (r *recordingEnroller) CreateVerified(ctx context.Context, groupID int64, in participants.Input, at time.Time) (*enrolment.Created, error) {
	r.ins = append(r.ins, in)
	return r.next.CreateVerified(ctx, groupID, in, at)
}

// recordEnrol remplace le port CreateVerified du service par un enregistreur.
func (e *env) recordEnrol() *recordingEnroller {
	rec := &recordingEnroller{next: e.svc.cfg.Enrol}
	e.svc.cfg.Enrol = rec
	return rec
}

// registerRole soumet le formulaire avec un choix de rôle (option active).
func (e *env) registerRole(t *testing.T, g int64, first, name, email string, role participants.DrawRole) registration {
	t.Helper()
	ctx := t.Context()
	if err := e.inv.SetRoleChoice(ctx, admin(g), g, true); err != nil {
		t.Fatal(err)
	}
	grp, err := e.groups.ByID(ctx, g)
	if err != nil {
		t.Fatal(err)
	}
	l, err := e.inv.Enable(ctx, admin(g), g)
	if err != nil {
		t.Fatal(err)
	}
	res, err := e.inv.Submit(ctx, grp.Slug, l.Token, invite.Submission{FirstName: first, Name: name, Email: email,
		IP: "203.0.113.7", Roles: []string{string(role)}})
	if err != nil {
		t.Fatal(err)
	}
	if res.Outcome != invite.OutcomeCodeSent {
		t.Fatalf("inscription refusée : %+v", res)
	}
	ps, err := e.pending.ListPending(ctx, g, e.clock.Now())
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range ps {
		if p.Ref == res.Ref {
			if p.Role != role {
				t.Fatalf("rôle en attente %q, %q attendu", p.Role, role)
			}
			return registration{group: g, slug: grp.Slug, id: p.ID, ref: p.Ref, email: email}
		}
	}
	t.Fatal("inscription en attente introuvable")
	return registration{}
}

func (e *env) roleOf(t *testing.T, g, id int64) participants.DrawRole {
	t.Helper()
	p, err := e.parts.Get(t.Context(), admin(g), g, id)
	if err != nil {
		t.Fatal(err)
	}
	return p.Role
}

// F11d.AC3 : le rôle choisi est conservé avec l'inscription en attente et
// transmis à CreateVerified à la vérification par code, par le lien en un
// clic et à la validation sans code par l'administrateur.
func TestRoleTransferredToCreateVerified(t *testing.T) {
	e := newEnv(t)
	ctx := t.Context()
	rec := e.recordEnrol()

	// Vérification par code.
	r1 := e.registerRole(t, e.g1, "Léa", "Martin", "lea@example.org", participants.RoleReceptionSeule)
	s1, err := e.svc.VerifyCode(ctx, e.g1, r1.ref, e.code(t, r1))
	if err != nil {
		t.Fatal(err)
	}
	if got := e.roleOf(t, e.g1, s1.ParticipantID); got != participants.RoleReceptionSeule {
		t.Fatalf("code : rôle %q", got)
	}

	// Lien de confirmation en un clic.
	r2 := e.registerRole(t, e.g1, "Noé", "Bernard", "noe@example.org", participants.RoleOffrePlus)
	s2, err := e.svc.ConfirmLink(ctx, e.g1, e.confirmToken(t, r2))
	if err != nil {
		t.Fatal(err)
	}
	if got := e.roleOf(t, e.g1, s2.ParticipantID); got != participants.RoleOffrePlus {
		t.Fatalf("lien : rôle %q", got)
	}

	// Validation sans code par l'administrateur.
	r3 := e.registerRole(t, e.g1, "Inès", "Petit", "ines@example.org", participants.RoleOffrePlus)
	v, err := e.svc.ValidateWithoutCode(ctx, admin(e.g1), e.g1, r3.id)
	if err != nil {
		t.Fatal(err)
	}
	if got := e.roleOf(t, e.g1, v.ParticipantID); got != participants.RoleOffrePlus {
		t.Fatalf("validation sans code : rôle %q", got)
	}

	// Sans choix (option active) : normal.
	r4 := e.registerRole(t, e.g1, "Tom", "Roux", "tom@example.org", participants.RoleNormal)
	s4, err := e.svc.VerifyCode(ctx, e.g1, r4.ref, e.code(t, r4))
	if err != nil {
		t.Fatal(err)
	}
	if got := e.roleOf(t, e.g1, s4.ParticipantID); got != participants.RoleNormal {
		t.Fatalf("normal : rôle %q", got)
	}

	want := []participants.DrawRole{participants.RoleReceptionSeule, participants.RoleOffrePlus,
		participants.RoleOffrePlus, participants.RoleNormal}
	if len(rec.ins) != len(want) {
		t.Fatalf("%d appels à CreateVerified, %d attendus", len(rec.ins), len(want))
	}
	for i, in := range rec.ins {
		if in.Role != want[i] {
			t.Fatalf("appel %d : rôle transmis %q, %q attendu", i, in.Role, want[i])
		}
	}
}

// F11d.AC3 : une inscription faite option désactivée crée un participant
// normal, même si l'option est activée avant la vérification.
func TestRoleKeptFromSubmissionTime(t *testing.T) {
	e := newEnv(t)
	ctx := t.Context()
	r := e.register(t, e.g1, "Léa", "Martin", "lea@example.org")
	if err := e.inv.SetRoleChoice(ctx, admin(e.g1), e.g1, true); err != nil {
		t.Fatal(err)
	}
	s, err := e.svc.VerifyCode(ctx, e.g1, r.ref, e.code(t, r))
	if err != nil {
		t.Fatal(err)
	}
	if got := e.roleOf(t, e.g1, s.ParticipantID); got != participants.RoleNormal {
		t.Fatalf("rôle %q, normal attendu", got)
	}
}

// F11d.AC3 : le rôle choisi n'est visible d'aucun autre participant : ni par
// la liste du groupe, ni par la fiche.
func TestRoleNotVisibleToOtherParticipants(t *testing.T) {
	e := newEnv(t)
	ctx := t.Context()
	ra := e.registerRole(t, e.g1, "Léa", "Martin", "lea@example.org", participants.RoleReceptionSeule)
	sa, err := e.svc.VerifyCode(ctx, e.g1, ra.ref, e.code(t, ra))
	if err != nil {
		t.Fatal(err)
	}
	rb := e.registerRole(t, e.g1, "Noé", "Bernard", "noe@example.org", participants.RoleOffrePlus)
	sb, err := e.svc.VerifyCode(ctx, e.g1, rb.ref, e.code(t, rb))
	if err != nil {
		t.Fatal(err)
	}

	other := participants.ParticipantActor(e.g1, sb.ParticipantID)
	list, err := e.parts.List(ctx, other, e.g1)
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range list {
		if p.ID == sa.ParticipantID && p.Role != "" {
			t.Fatalf("le rôle de Léa est visible de Noé : %q", p.Role)
		}
	}
	if _, err := e.parts.Get(ctx, other, e.g1, sa.ParticipantID); !errs.Is(err, errs.KindForbidden) {
		t.Fatalf("fiche de Léa lue par Noé : %v", err)
	}

}
