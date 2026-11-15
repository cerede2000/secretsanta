package people

import (
	"strings"
	"testing"
	"time"

	"secretsanta/internal/participants"
)

func (e *env) queueRow(t *testing.T, gid, pid int64, status string, at time.Time) {
	t.Helper()
	var sent any
	if status == "sent" {
		sent = at.UnixNano()
	}
	if _, err := e.db.Exec(`INSERT INTO mail_queue (group_id, participant_id, recipient, mail_type, essential, status, created_at, updated_at, send_after, sent_at)
		VALUES (?, ?, 'x@exemple.fr', 'invitation', 1, ?, ?, ?, ?, ?)`, gid, pid, status, at.UnixNano(), at.UnixNano(), at.UnixNano(), sent); err != nil {
		t.Fatal(err)
	}
}

func (e *env) openSession(t *testing.T, gid, pid int64, at time.Time) {
	t.Helper()
	if _, err := e.db.Exec(`INSERT INTO websec_sessions (id_hash, kind, group_id, subject, created_at, expires_at)
		VALUES (?, 'participant', ?, ?, ?, ?)`, "h"+sid(pid), sid(gid), sid(pid), at.UnixNano(), at.Add(time.Hour).UnixNano()); err != nil {
		t.Fatal(err)
	}
}

func TestSheetShowsFullAccessStatusAndNeverTheTarget(t *testing.T) {
	e := newEnv(t)
	a := e.person(t, e.groupA, "Alice", "alice@exemple.fr", participants.RoleNormal)
	b := e.person(t, e.groupA, "Bruno", "bruno@exemple.fr", participants.RoleNormal)
	c := e.person(t, e.groupA, "Chloe", "", participants.RoleNormal)
	e.seedDraw(t, e.groupA, []int64{a, b, c}) // Alice offre à Bruno
	e.queueRow(t, e.groupA, a, "sent", t0)
	e.openSession(t, e.groupA, a, t0.Add(2*time.Hour))
	if _, err := e.db.Exec(`UPDATE draw_pairs SET discovered_at = ? WHERE giver_id = ?`, t0.Format(time.RFC3339Nano), a); err != nil {
		t.Fatal(err)
	}
	e.queueRow(t, e.groupA, b, "failed", t0)
	ck := e.adminCookies(t, e.groupA)

	body := e.get(t, "/noel/admin/participants/"+sid(a), "fr", ck).Body.String()
	for _, want := range []string{"Statut d&#39;accès", "Courriel envoyé", "2026-11-15 10:00", "Dernière connexion", "2026-11-15 12:00", "Révélation faite", "Oui", "Lien ouvert"} {
		if !strings.Contains(body, want) {
			t.Errorf("fiche d'Alice : %q absent", want)
		}
	}
	// La cible d'Alice (Bruno) n'apparaît nulle part, ni son identité.
	for _, leak := range []string{"Bruno", "Zx" + "Bruno", "bruno@exemple.fr", "Chloe"} {
		if strings.Contains(body, leak) {
			t.Errorf("la fiche d'Alice contient %q", leak)
		}
	}

	failed := e.get(t, "/noel/admin/participants/"+sid(b), "fr", ck).Body.String()
	if !strings.Contains(failed, "Échec d&#39;envoi") || !strings.Contains(failed, "Corrige l&#39;adresse") {
		t.Error("échec d'envoi non signalé")
	}
	if !strings.Contains(failed, "Pas encore") || !strings.Contains(failed, "Jamais") {
		t.Error("état « jamais ouvert » ou « pas encore » absent")
	}
	// Une personne sans courriel n'a pas de bouton « Renvoyer ».
	none := e.get(t, "/noel/admin/participants/"+sid(c), "fr", ck).Body.String()
	if strings.Contains(none, "/renvoyer\"") {
		t.Error("renvoi proposé à une personne sans courriel")
	}
}

func TestSheetNeedsTheAdminSessionOfTheSameGroup(t *testing.T) {
	e := newEnv(t)
	a := e.person(t, e.groupA, "Alice", "alice@exemple.fr", participants.RoleNormal)
	if rec := e.get(t, "/noel/admin/participants/"+sid(a), "fr", nil); rec.Code != 303 {
		t.Errorf("sans session : 303 attendu, %d", rec.Code)
	}
	other := e.adminCookies(t, e.groupB)
	if rec := e.get(t, "/noel/admin/participants/"+sid(a), "fr", other); rec.Code != 303 {
		t.Errorf("session d'un autre groupe : 303 attendu, %d", rec.Code)
	}
	own := e.adminCookies(t, e.groupB)
	b := e.person(t, e.groupB, "Zoe", "zoe@exemple.fr", participants.RoleNormal)
	if rec := e.get(t, "/paques/admin/participants/"+sid(a), "fr", own); rec.Code != 404 {
		t.Errorf("personne d'un autre groupe : 404 attendu, %d", rec.Code)
	}
	if rec := e.get(t, "/paques/admin/participants/"+sid(b), "fr", own); rec.Code != 200 {
		t.Errorf("sa propre fiche : 200 attendu, %d", rec.Code)
	}
}
