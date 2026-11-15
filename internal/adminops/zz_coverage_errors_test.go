package adminops_test

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"secretsanta/internal/adminops"
	"secretsanta/internal/shared/clock"
)

func TestZZServiceRefusals(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	if _, err := adminops.NewService(nil, f.clock, adminops.Options{}); err == nil {
		t.Error("base absente acceptée")
	}
	if _, err := adminops.NewService(f.db, clock.Clock(nil), adminops.Options{}); err == nil {
		t.Error("horloge absente acceptée")
	}
	const unknown = 987654
	var buf bytes.Buffer
	if err := f.ops.Backup(ctx, unknown, &buf); err == nil {
		t.Error("sauvegarde d'un groupe inconnu acceptée")
	}
	if at, err := f.ops.LastBackup(ctx, unknown); err != nil || at != nil {
		t.Errorf("dernière sauvegarde : %v %v", at, err)
	}
	if _, err := f.ops.ResetAdminSecret(ctx, unknown); err == nil {
		t.Error("réinitialisation d'un groupe inconnu acceptée")
	}
	if _, err := f.ops.PlanDuplicate(ctx, unknown); err == nil {
		t.Error("plan d'un groupe inconnu accepté")
	}
	if _, err := f.ops.Duplicate(ctx, unknown); err == nil {
		t.Error("duplication d'un groupe inconnu acceptée")
	}
	if err := f.ops.DeleteGroup(ctx, unknown, adminops.DeleteInput{ConfirmName: "x"}); err == nil {
		t.Error("suppression d'un groupe inconnu acceptée")
	}
	// L export d un groupe inconnu rend un fichier vide, sans erreur.
	if x, err := f.ops.Export(ctx, unknown); err != nil || x == nil {
		t.Errorf("export vide : %v", err)
	}
	gid, _ := f.group(t, "ops", "Famille 2025", "2025-12-24")
	if _, err := f.ops.Import(ctx, gid, strings.NewReader("pas du json")); err == nil {
		t.Error("import invalide accepté")
	}
	if _, err := f.ops.Import(ctx, unknown, strings.NewReader(`{}`)); err == nil {
		t.Error("import dans un groupe inconnu accepté")
	}
	if err := f.ops.DeleteGroup(ctx, gid, adminops.DeleteInput{}); err == nil {
		t.Error("suppression sans confirmation acceptée")
	}
	if err := f.ops.DeleteGroup(ctx, gid, adminops.DeleteInput{ConfirmName: "Autre nom"}); err == nil {
		t.Error("suppression avec mauvais nom acceptée")
	}
	if p, err := f.ops.PlanDuplicate(ctx, gid); err != nil || p == nil {
		t.Errorf("plan : %v %v", p, err)
	}
	// Dupliquer deux fois : les noms et adresses se décalent.
	for i := 0; i < 3; i++ {
		if _, err := f.ops.Duplicate(ctx, gid); err != nil {
			t.Fatalf("duplication %d : %v", i, err)
		}
	}
}

func TestZZServiceClosedDatabase(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	gid, _ := f.group(t, "ferme", "Famille 2025", "2025-12-24")
	_ = f.db.Close()
	var buf bytes.Buffer
	if f.ops.Backup(ctx, gid, &buf) == nil {
		t.Error("Backup")
	}
	if _, err := f.ops.LastBackup(ctx, gid); err == nil {
		t.Error("LastBackup")
	}
	if _, err := f.ops.ResetAdminSecret(ctx, gid); err == nil {
		t.Error("ResetAdminSecret")
	}
	if _, err := f.ops.PlanDuplicate(ctx, gid); err == nil {
		t.Error("PlanDuplicate")
	}
	if _, err := f.ops.Duplicate(ctx, gid); err == nil {
		t.Error("Duplicate")
	}
	if f.ops.DeleteGroup(ctx, gid, adminops.DeleteInput{ConfirmName: "x"}) == nil {
		t.Error("DeleteGroup")
	}
	if _, err := f.ops.Export(ctx, gid); err == nil {
		t.Error("Export")
	}
	if f.ops.ExportJSON(ctx, gid, &buf) == nil {
		t.Error("ExportJSON")
	}
	if _, err := f.ops.Import(ctx, gid, strings.NewReader(`{}`)); err == nil {
		t.Error("Import")
	}
}

func TestZZPagesFailures(t *testing.T) {
	if _, err := adminops.New(adminops.Deps{}); err == nil {
		t.Error("dépendances absentes acceptées")
	}
	e := newWebEnv(t)
	// Mauvais nom : refus intégré à la page (422), le groupe reste.
	r := e.postForm(t, "/famille/admin/supprimer", "", url.Values{"confirm_name": {"Mauvais"}})
	if r.Code != http.StatusUnprocessableEntity {
		t.Errorf("suppression refusée : %d", r.Code)
	}
	// Fichier d'import invalide.
	if r := e.postFile(t, "/famille/admin/participants/import", "", []byte("pas du json")); r.Code < 400 {
		t.Errorf("import invalide : %d", r.Code)
	}
	if r := e.postFile(t, "/famille/admin/participants/import", "", nil); r.Code < 400 {
		t.Errorf("import sans fichier : %d", r.Code)
	}
	// Sans session : renvoi vers la connexion ; groupe inconnu : 404.
	if r := e.request("GET", "/famille/admin/donnees", "", "", nil, nil); r.Code != http.StatusSeeOther {
		t.Errorf("sans session : %d", r.Code)
	}
	if r := e.get("/inconnu/admin/donnees", ""); r.Code != http.StatusNotFound {
		t.Errorf("groupe inconnu : %d", r.Code)
	}
	// Panne : la table des sauvegardes disparaît.
	if _, err := e.db.Exec(`DROP TABLE adminops_backups`); err != nil {
		t.Fatal(err)
	}
	if r := e.get("/famille/admin/donnees", ""); r.Code != http.StatusInternalServerError {
		t.Errorf("page en panne : %d", r.Code)
	}
	if r := e.postForm2(t, "/famille/admin/sauvegarde"); r.Code != http.StatusInternalServerError {
		t.Logf("sauvegarde en panne : %d", r.Code)
	}
}

// postForm2 poste sans passer par la page « Données » (en panne) : le jeton est
// pris sur la page de connexion de la même origine.
func (e *webEnv) postForm2(t *testing.T, target string) *httptest.ResponseRecorder {
	t.Helper()
	return e.request("POST", target, "", "application/x-www-form-urlencoded", strings.NewReader(""), e.cookies)
}

func TestZZDuplicateLongNames(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	long := strings.Repeat("é", 80)
	gid, _ := f.group(t, "tres-long-nom-sans-annee-adresse-groupe", long, "")
	res, err := f.ops.Duplicate(ctx, gid)
	if err != nil {
		t.Fatal(err)
	}
	if n := len([]rune(res.Name)); n > 80 || res.Slug == "" {
		t.Errorf("nom %d caractères, adresse %q", n, res.Slug)
	}
	// Une seconde copie du même groupe prend une autre adresse.
	res2, err := f.ops.Duplicate(ctx, gid)
	if err != nil || res2.Slug == res.Slug {
		t.Errorf("seconde copie : %v %q %q", err, res.Slug, res2.Slug)
	}
}
