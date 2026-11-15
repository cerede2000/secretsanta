package drawpages

import (
	"net/http"
	"net/url"
	"strings"
	"testing"

	"secretsanta/internal/participants"
	"secretsanta/internal/shared/ports"
)

// F10.AC22 : avec des inscriptions en attente, la page affiche leur nombre et
// exige le choix « ignorer » ou « supprimer » avant d'envoyer le lancement.
func TestPendingRegistrationsRequireExplicitChoice(t *testing.T) {
	e := newEnv(t)
	gid := e.group("attente")
	e.addN(gid, "Hortensia", 4, participants.RoleNormal)
	e.reg.Pending[gid] = 3
	ck := e.adminCookie(gid)

	page := e.get("/attente/admin/tirage", "fr", ck)
	if page.Code != 200 || !strings.Contains(page.Body.String(), "3 inscriptions") {
		t.Fatalf("le nombre d'inscriptions en attente doit s'afficher : %d", page.Code)
	}
	panel := e.get("/attente/admin/tirage?confirmer=lancer", "fr", ck).Body.String()
	for _, want := range []string{`name="pending" value="ignorer"`, `name="pending" value="supprimer"`, "required", `action="/attente/admin/tirage/lancer"`} {
		if !strings.Contains(panel, want) {
			t.Errorf("volet de lancement sans %q", want)
		}
	}
	e.noNames(t, "page", panel)

	// Sans choix : refus, aucun tirage, rien de résolu.
	rec := e.post("/attente/admin/tirage/lancer", "/attente/admin/tirage?confirmer=lancer", ck, url.Values{"confirm": {"1"}})
	if rec.Code != http.StatusUnprocessableEntity || !strings.Contains(rec.Body.String(), `role="alert"`) {
		t.Fatalf("sans choix : %d", rec.Code)
	}
	if e.drawCount(gid) != 0 || len(e.reg.Resolved) != 0 {
		t.Fatal("rien ne devait être lancé ni résolu sans choix")
	}
	// Choix invalide.
	rec = e.post("/attente/admin/tirage/lancer", "/attente/admin/tirage?confirmer=lancer", ck, url.Values{"confirm": {"1"}, "pending": {"peut-etre"}})
	if rec.Code != http.StatusUnprocessableEntity || e.drawCount(gid) != 0 {
		t.Fatalf("choix invalide : %d", rec.Code)
	}
	// Sans confirmation : refus aussi.
	rec = e.post("/attente/admin/tirage/lancer", "/attente/admin/tirage?confirmer=lancer", ck, url.Values{"pending": {"ignorer"}})
	if rec.Code != http.StatusUnprocessableEntity || e.drawCount(gid) != 0 {
		t.Fatalf("sans confirmation : %d", rec.Code)
	}
	// Avec le choix : résolu puis tiré.
	rec = e.post("/attente/admin/tirage/lancer", "/attente/admin/tirage?confirmer=lancer", ck, url.Values{"confirm": {"1"}, "pending": {"supprimer"}})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("avec choix : %d %s", rec.Code, rec.Body.String())
	}
	if e.reg.Resolved[gid] != ports.ResolveDelete {
		t.Fatalf("choix non appliqué : %v", e.reg.Resolved)
	}
	if e.drawCount(gid) != 1 || e.changed != 1 {
		t.Fatalf("tirage attendu : %d, crochet %d", e.drawCount(gid), e.changed)
	}
	if countRows(t, e.db, `SELECT COUNT(*) FROM admin_audit WHERE group_id = ? AND action = 'tirage_lance'`, gid) != 1 {
		t.Fatal("le lancement doit être consigné")
	}
	// Après le tirage, la page n'affiche aucune paire ni aucun nom.
	after := e.get("/attente/admin/tirage", "fr", ck)
	e.noNames(t, "page après tirage", after.Body.String())
}

// Sans inscription en attente, aucun choix n'est demandé.
func TestNoPendingNoChoice(t *testing.T) {
	e := newEnv(t)
	gid := e.group("sansattente")
	e.addN(gid, "Hortensia", 4, participants.RoleNormal)
	ck := e.adminCookie(gid)
	if body := e.get("/sansattente/admin/tirage?confirmer=lancer", "fr", ck).Body.String(); strings.Contains(body, `name="pending"`) {
		t.Fatal("aucun choix attendu sans inscription en attente")
	}
	rec := e.post("/sansattente/admin/tirage/lancer", "/sansattente/admin/tirage?confirmer=lancer", ck, url.Values{"confirm": {"1"}})
	if rec.Code != http.StatusSeeOther || e.drawCount(gid) != 1 {
		t.Fatalf("lancement : %d", rec.Code)
	}
	// Refaire : archive l'ancien, deux tirages dans l'historique.
	rec = e.post("/sansattente/admin/tirage/lancer", "/sansattente/admin/tirage?confirmer=lancer", ck, url.Values{"confirm": {"1"}})
	if rec.Code != http.StatusSeeOther || e.drawCount(gid) != 2 {
		t.Fatalf("nouveau tirage : %d", rec.Code)
	}
	hist := e.get("/sansattente/admin/tirage?onglet=historique", "fr", ck)
	if hist.Code != 200 || !strings.Contains(hist.Body.String(), "2026") {
		t.Fatalf("historique : %d", hist.Code)
	}
	e.noNames(t, "historique", hist.Body.String())
}

// Une session d'un autre groupe ne donne aucun accès.
func TestOtherGroupSessionRefused(t *testing.T) {
	e := newEnv(t)
	a, b := e.group("groupe-a"), e.group("groupe-b")
	e.addN(a, "Hortensia", 3, participants.RoleNormal)
	ck := e.adminCookie(b)
	if rec := e.get("/groupe-a/admin/tirage", "fr", ck); rec.Code != http.StatusSeeOther {
		t.Fatalf("GET autre groupe : %d", rec.Code)
	}
	if rec := e.get("/inconnu/admin/tirage", "fr", ck); rec.Code != http.StatusNotFound {
		t.Fatalf("groupe inconnu : %d", rec.Code)
	}
}
