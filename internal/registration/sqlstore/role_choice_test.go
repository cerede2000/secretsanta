package sqlstore

import (
	"database/sql"
	"testing"

	"secretsanta/internal/db"
	"secretsanta/internal/groups"
	"secretsanta/internal/participants"
	"secretsanta/internal/registration/invite"
	"secretsanta/internal/registration/migrations"
)

// L'option « choix du rôle » survit à un autre magasin sur la même base, au
// renouvellement du lien et à l'arrêt de l'option ; sans ligne, elle est
// désactivée.
func TestRoleChoicePersistsOverSQL(t *testing.T) {
	e := newEnv(t)
	ctx := t.Context()
	e.enable(t, e.g1)
	if inv, _ := NewInviteStore(e.db).Invitation(ctx, e.g1); inv.RoleChoice {
		t.Fatal("option active par défaut")
	}
	if err := e.svc.SetRoleChoice(ctx, participants.AdminActor(e.g1), e.g1, true); err != nil {
		t.Fatal(err)
	}
	if inv, err := NewInviteStore(e.db).Invitation(ctx, e.g1); err != nil || !inv.RoleChoice {
		t.Fatalf("option non relue : %+v %v", inv, err)
	}
	if inv, _ := e.invites.Invitation(ctx, e.g2); inv.RoleChoice {
		t.Fatal("l'option d'un groupe a gagné un autre groupe")
	}
	if _, err := e.svc.Renew(ctx, participants.AdminActor(e.g1), e.g1); err != nil {
		t.Fatal(err)
	}
	if inv, _ := e.invites.Invitation(ctx, e.g1); !inv.RoleChoice {
		t.Fatal("renouveler le lien a effacé l'option")
	}
	if err := e.svc.SetRoleChoice(ctx, participants.AdminActor(e.g1), e.g1, false); err != nil {
		t.Fatal(err)
	}
	if inv, _ := e.invites.Invitation(ctx, e.g1); inv.RoleChoice {
		t.Fatal("option toujours active après désactivation")
	}
}

// Le rôle choisi à l'inscription est enregistré avec l'inscription et relu tel
// quel ; il vaut normal quand l'option est désactivée ou quand rien n'est
// choisi.
func TestPendingRoleOverSQL(t *testing.T) {
	e := newEnv(t)
	ctx := t.Context()
	l := e.enable(t, e.g1)
	if err := e.svc.SetRoleChoice(ctx, participants.AdminActor(e.g1), e.g1, true); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		first, email string
		roles        []string
		want         participants.DrawRole
	}{
		{"Léa", "lea@example.org", []string{"offre_plus"}, participants.RoleOffrePlus},
		{"Hugo", "hugo@example.org", []string{"reception_seule"}, participants.RoleReceptionSeule},
		{"Zoé", "zoe@example.org", nil, participants.RoleNormal},
	} {
		sub := submission(tc.first, "Martin", tc.email)
		sub.Roles = tc.roles
		r := e.submit(t, "noel", l.Token, sub)
		if r.Outcome != invite.OutcomeCodeSent {
			t.Fatalf("%s : issue %s", tc.first, r.Outcome)
		}
		if got := e.pendingByRef(t, e.g1, r.Ref).Role; got != tc.want {
			t.Errorf("%s : rôle %q, attendu %q", tc.first, got, tc.want)
		}
	}
	// Option désactivée : toute valeur envoyée est ignorée.
	if err := e.svc.SetRoleChoice(ctx, participants.AdminActor(e.g1), e.g1, false); err != nil {
		t.Fatal(err)
	}
	sub := submission("Max", "Durand", "max@example.org")
	sub.Roles = []string{"offre_plus"}
	r := e.submit(t, "noel", l.Token, sub)
	if r.Outcome != invite.OutcomeCodeSent {
		t.Fatalf("issue %s", r.Outcome)
	}
	if got := e.pendingByRef(t, e.g1, r.Ref).Role; got != participants.RoleNormal {
		t.Fatalf("option désactivée : rôle %q", got)
	}
}

// Un rôle vide est enregistré comme normal ; un rôle non permis à
// l'inscription (un_seul) ou inconnu est refusé par la base.
func TestPendingRoleEmptyAndRefusedOverSQL(t *testing.T) {
	e := newEnv(t)
	ctx := t.Context()
	base := invite.Pending{
		GroupID: e.g1, Ref: "r1", FirstName: "Léa", Name: "Martin", NameKey: "lea martin",
		Email: "lea@example.org", EmailKey: "lea@example.org", CreatedAt: t0, ExpiresAt: t0.Add(invite.PendingTTL),
	}
	p, err := e.invites.AddPending(ctx, base)
	if err != nil {
		t.Fatal(err)
	}
	if p.Role != participants.RoleNormal {
		t.Fatalf("rôle vide renvoyé %q", p.Role)
	}
	if ps, _ := e.invites.ListPending(ctx, e.g1, t0); len(ps) != 1 || ps[0].Role != participants.RoleNormal {
		t.Fatalf("rôle vide relu : %+v", ps)
	}
	for _, bad := range []participants.DrawRole{participants.RoleUnSeul, "admin"} {
		b := base
		b.Ref, b.Role = "r2", bad
		if _, err := e.invites.AddPending(ctx, b); err == nil {
			t.Errorf("rôle %q accepté", bad)
		}
	}
}

// La migration 003 s'applique à une base déjà peuplée : les lignes existantes
// gardent l'option désactivée et le rôle normal, et la base refuse ensuite
// une valeur hors contrat.
func TestRoleChoiceMigrationKeepsExistingRows(t *testing.T) {
	d, err := db.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	ms, err := migrations.Migrations()
	if err != nil {
		t.Fatal(err)
	}
	if len(ms) < 3 {
		t.Fatalf("%d migrations, la 003 manque", len(ms))
	}
	// Tout sauf la dernière migration, puis des données d'avant.
	prior := ms[:2]
	migrateBefore(t, d, prior)
	g := insertGroup(t, d, "ancien")
	if _, err := d.Exec(`INSERT INTO registration_invitations (group_id, enabled, capacity, updated_at) VALUES (?, 1, 5, 0)`, g); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Exec(`INSERT INTO registration_pending (group_id, ref, first_name, name, name_key, email, email_key, created_at, expires_at)
		VALUES (?, 'r', 'Léa', 'Martin', 'lea martin', 'l@e.org', 'l@e.org', 1, 2)`, g); err != nil {
		t.Fatal(err)
	}
	if err := db.Migrate(d, migrations.Module, ms); err != nil {
		t.Fatalf("migration 003 : %v", err)
	}
	var choice int
	var role string
	if err := d.QueryRow(`SELECT role_choice FROM registration_invitations WHERE group_id = ?`, g).Scan(&choice); err != nil || choice != 0 {
		t.Fatalf("role_choice %d %v", choice, err)
	}
	if err := d.QueryRow(`SELECT role FROM registration_pending WHERE group_id = ?`, g).Scan(&role); err != nil || role != "normal" {
		t.Fatalf("role %q %v", role, err)
	}
	if _, err := d.Exec(`UPDATE registration_invitations SET role_choice = 2 WHERE group_id = ?`, g); err == nil {
		t.Error("role_choice hors 0/1 accepté")
	}
	if _, err := d.Exec(`UPDATE registration_pending SET role = 'un_seul' WHERE group_id = ?`, g); err == nil {
		t.Error("rôle un_seul accepté")
	}
}

// migrateBefore applique les migrations des groupes et des participants, puis
// seulement les migrations d'inscription données.
func migrateBefore(t *testing.T, d *sql.DB, reg []db.Migration) {
	t.Helper()
	gm, err := groups.Migrations()
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Migrate(d, groups.Module, gm); err != nil {
		t.Fatal(err)
	}
	pm, err := participants.Migrations()
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Migrate(d, participants.Module, pm); err != nil {
		t.Fatal(err)
	}
	if err := db.Migrate(d, migrations.Module, reg); err != nil {
		t.Fatal(err)
	}
}
