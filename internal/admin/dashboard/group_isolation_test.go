package dashboard

import (
	"context"
	"net/http"
	"secretsanta/internal/draw"
	"strconv"
	"testing"

	"secretsanta/internal/participants"
	"secretsanta/internal/shared/errs"
)

// F10.AC4 : l'administrateur d'un groupe ne peut agir sur aucun autre groupe.
func TestAdminCannotActOnAnotherGroupThroughTheService(t *testing.T) {
	e := newEnv(t)
	a := e.group("groupe-a")
	b := e.group("groupe-b")
	e.addN(a, "Hortensia", 3, participants.RoleNormal)
	pb := e.add(b, "Mamie", "", participants.RoleNormal)
	e.addN(b, "Gaspardin", 2, participants.RoleNormal)
	e.doDraw(b)

	adminA := admin(a)
	if _, err := e.svc.Dashboard(ctx, adminA, b); !errs.Is(err, errs.KindForbidden) {
		t.Errorf("tableau de bord d'un autre groupe : %v", err)
	}
	if _, err := e.svc.CheckDraw(ctx, adminA, b); !errs.Is(err, errs.KindForbidden) {
		t.Errorf("contrôle d'un autre groupe : %v", err)
	}
	if err := e.svc.MarkLinkHandedOver(ctx, adminA, b, pb); !errs.Is(err, errs.KindForbidden) {
		t.Errorf("remise dans un autre groupe : %v", err)
	}
	// Même en prétendant agir dans son propre groupe, la personne d'un autre
	// groupe est introuvable.
	if err := e.svc.MarkLinkHandedOver(ctx, adminA, a, pb); !errs.Is(err, errs.KindNotFound) {
		t.Errorf("personne d'un autre groupe : %v", err)
	}
	if _, err := e.audit.List(ctx, adminA, b, 10); !errs.Is(err, errs.KindForbidden) {
		t.Errorf("journal d'un autre groupe : %v", err)
	}
	// Ni un participant, ni un anonyme n'agissent comme administrateur.
	for _, actor := range []participants.Actor{participants.ParticipantActor(b, pb), {}} {
		if _, err := e.svc.Dashboard(ctx, actor, b); err == nil {
			t.Errorf("l'acteur %+v ne doit pas voir le tableau de bord", actor)
		}
		if err := e.svc.MarkLinkHandedOver(ctx, actor, b, pb); err == nil {
			t.Errorf("l'acteur %+v ne doit pas agir", actor)
		}
	}
	if n := countRows(t, e.db, `SELECT COUNT(*) FROM admin_link_handover`); n != 0 {
		t.Fatalf("marques écrites malgré les refus : %d", n)
	}
	if n := countRows(t, e.db, `SELECT COUNT(*) FROM admin_audit`); n != 0 {
		t.Fatalf("lignes de journal écrites malgré les refus : %d", n)
	}
}

func TestDashboardOfOneGroupNeverListsAnotherGroup(t *testing.T) {
	e := newEnv(t)
	a := e.group("groupe-a")
	b := e.group("groupe-b")
	e.add(a, "Hortensia", "", participants.RoleNormal)
	e.add(b, "Gaspardin", "", participants.RoleNormal)
	v, err := e.svc.Dashboard(ctx, admin(a), a)
	if err != nil {
		t.Fatal(err)
	}
	if v.Registered != 1 || len(v.People) != 1 || v.People[0].FirstName != "Hortensia" {
		t.Fatalf("personnes : %+v", v.People)
	}
	_ = b
}

func TestAdminSessionOfAnotherGroupIsRefusedByThePages(t *testing.T) {
	e := newEnv(t)
	a := e.group("groupe-a")
	b := e.group("groupe-b")
	e.add(a, "Hortensia", "", participants.RoleNormal)
	pb := e.add(b, "Mamie", "", participants.RoleNormal)
	e.addN(b, "Gaspardin", 2, participants.RoleNormal)
	e.doDraw(b)
	ckA := e.adminCookie(a)
	ckB := e.adminCookie(b)

	// La session de A n'ouvre aucune page de B : renvoi vers la connexion de B.
	for _, p := range []string{"/groupe-b/admin", "/groupe-b/admin/controle", "/groupe-b/admin/journal"} {
		rec := e.get(p, "fr", ckA)
		if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/groupe-b/admin/connexion" {
			t.Errorf("%s avec la session de A : %d %s", p, rec.Code, rec.Header().Get("Location"))
		}
		if rec.Body.Len() > 200 {
			t.Errorf("%s laisse fuiter du contenu", p)
		}
	}
	// Une action modifiante de A sur B est refusée et ne change rien.
	path := "/groupe-b/admin/personnes/" + strconv.FormatInt(pb, 10) + "/lien-remis"
	rec := e.post(path, "/groupe-a/admin", ckA)
	if rec.Code != http.StatusForbidden {
		t.Errorf("POST de A sur B : %d", rec.Code)
	}
	// La personne de B ne peut pas non plus être atteinte par la route de A.
	rec = e.post("/groupe-a/admin/personnes/"+strconv.FormatInt(pb, 10)+"/lien-remis", "/groupe-a/admin", ckA)
	if rec.Code != http.StatusNotFound {
		t.Errorf("personne de B par la route de A : %d", rec.Code)
	}
	if n := countRows(t, e.db, `SELECT COUNT(*) FROM admin_link_handover`); n != 0 {
		t.Fatalf("marques écrites : %d", n)
	}
	if n := countRows(t, e.db, `SELECT COUNT(*) FROM admin_audit`); n != 0 {
		t.Fatalf("journal écrit : %d", n)
	}
	// Sans session, mêmes refus.
	if rec := e.get("/groupe-b/admin", "fr", nil); rec.Code != http.StatusSeeOther {
		t.Errorf("sans session : %d", rec.Code)
	}
	// La bonne session fonctionne.
	if rec := e.get("/groupe-b/admin", "fr", ckB); rec.Code != http.StatusOK {
		t.Errorf("session de B : %d", rec.Code)
	}
	// Un groupe inconnu reste neutre.
	if rec := e.get("/inconnu/admin", "fr", ckA); rec.Code != http.StatusNotFound {
		t.Errorf("groupe inconnu : %d", rec.Code)
	}
}

// permissive ne contrôle pas l'acteur : il prouve que le service du tableau de
// bord se garde lui-même, sans compter sur les services qu'il appelle.
type permissive struct{ *participants.Service }

func (p permissive) List(c context.Context, _ participants.Actor, groupID int64) ([]participants.Participant, error) {
	return p.Service.List(c, participants.AdminActor(groupID), groupID)
}

type permissiveDraws struct{ *draw.Service }

func (p permissiveDraws) AdminFacts(c context.Context, _ participants.Actor, groupID int64) (*draw.Facts, error) {
	return p.Service.AdminFacts(c, participants.AdminActor(groupID), groupID)
}

func TestServiceGuardsItselfWithoutTheServicesItCalls(t *testing.T) {
	e := newEnv(t)
	a := e.group("groupe-a")
	b := e.group("groupe-b")
	pb := e.add(b, "Mamie", "", participants.RoleNormal)
	e.addN(b, "Gaspardin", 2, participants.RoleNormal)
	e.doDraw(b)
	svc, err := New(Options{DB: e.db, Clock: e.clock, Participants: permissive{e.parts}, Draws: permissiveDraws{e.draws},
		Constraints: e.rules, Audit: e.audit})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Dashboard(ctx, admin(a), b); !errs.Is(err, errs.KindForbidden) {
		t.Errorf("tableau de bord : %v", err)
	}
	if _, err := svc.CheckDraw(ctx, admin(a), b); !errs.Is(err, errs.KindForbidden) {
		t.Errorf("contrôle : %v", err)
	}
	if err := svc.MarkLinkHandedOver(ctx, admin(a), b, pb); !errs.Is(err, errs.KindForbidden) {
		t.Errorf("remise : %v", err)
	}
	if n := countRows(t, e.db, `SELECT COUNT(*) FROM admin_link_handover`); n != 0 {
		t.Fatalf("marques : %d", n)
	}
}
