package people

import (
	"net/url"
	"strings"
	"testing"

	"secretsanta/internal/participants"
)

func TestResendQueuesAnEmailAndIsJournaled(t *testing.T) {
	e := newEnv(t)
	a := e.person(t, e.groupA, "Alice", "alice@exemple.fr", participants.RoleNormal)
	ck := e.adminCookies(t, e.groupA)
	rec := e.post(t, "/noel/admin/participants/"+sid(a)+"/renvoyer", url.Values{}, "fr", ck)
	if rec.Code != 303 {
		t.Fatalf("303 attendu, %d", rec.Code)
	}
	if len(e.mail.items) != 1 {
		t.Fatalf("un courriel attendu en file, %d", len(e.mail.items))
	}
	it := e.mail.items[0]
	if it.To != "alice@exemple.fr" || it.GroupID != e.groupA || it.ParticipantID != a || !it.Essential {
		t.Errorf("courriel inattendu : %+v", it)
	}
	if !strings.Contains(it.Text, "https://noel.example.org/noel/acces/") {
		t.Error("le courriel ne porte pas le lien personnel")
	}
	if got := strings.Join(e.auditActions(t, e.groupA), ","); got != ActionLinkResent {
		t.Errorf("journal : %q", got)
	}
}

func TestResendRefusedWithoutEmailOrAcrossGroups(t *testing.T) {
	e := newEnv(t)
	c := e.person(t, e.groupA, "Chloe", "", participants.RoleNormal)
	a := e.person(t, e.groupA, "Alice", "alice@exemple.fr", participants.RoleNormal)
	ck := e.adminCookies(t, e.groupA)
	if rec := e.post(t, "/noel/admin/participants/"+sid(c)+"/renvoyer", url.Values{}, "fr", ck); rec.Code != 400 {
		t.Errorf("sans courriel : 400 attendu, %d", rec.Code)
	}
	other := e.adminCookies(t, e.groupB)
	e.group(t, "autre")
	if rec := e.post(t, "/paques/admin/participants/"+sid(a)+"/renvoyer", url.Values{}, "fr", other); rec.Code != 404 {
		t.Errorf("autre groupe : 404 attendu, %d", rec.Code)
	}
	if len(e.mail.items) != 0 || len(e.auditActions(t, e.groupA)) != 0 {
		t.Error("aucun courriel ni journal attendu")
	}
}

func TestCorrectAddressThenResendIsJournaled(t *testing.T) {
	e := newEnv(t)
	a := e.person(t, e.groupA, "Alice", "alice@exempel.fr", participants.RoleNormal)
	ck := e.adminCookies(t, e.groupA)
	rec := e.post(t, "/noel/admin/participants/"+sid(a)+"/adresse", url.Values{"email": {"alice@exemple.fr"}}, "fr", ck)
	if rec.Code != 303 {
		t.Fatalf("303 attendu, %d", rec.Code)
	}
	var email string
	if err := e.db.QueryRow(`SELECT email FROM participants WHERE id = ?`, a).Scan(&email); err != nil || email != "alice@exemple.fr" {
		t.Fatalf("adresse : %q %v", email, err)
	}
	if len(e.mail.items) != 1 || e.mail.items[0].To != "alice@exemple.fr" {
		t.Fatalf("courriel à la nouvelle adresse attendu : %+v", e.mail.items)
	}
	got := strings.Join(e.auditActions(t, e.groupA), ",")
	if got != ActionAddressFix+","+ActionLinkResent {
		t.Errorf("journal : %q", got)
	}
	// Adresse invalide : message sous le champ, rien n'est renvoyé.
	bad := e.post(t, "/noel/admin/participants/"+sid(a)+"/adresse", url.Values{"email": {"pas-une-adresse"}}, "fr", ck)
	body := bad.Body.String()
	if bad.Code != 400 || !strings.Contains(body, `aria-invalid="true"`) || !strings.Contains(body, `role="alert"`) {
		t.Errorf("erreur sous le champ attendue : %d", bad.Code)
	}
	if len(e.mail.items) != 1 {
		t.Error("un courriel est parti malgré l'adresse invalide")
	}
}

func TestThereIsNoViewAsRoute(t *testing.T) {
	e := newEnv(t)
	a := e.person(t, e.groupA, "Alice", "alice@exemple.fr", participants.RoleNormal)
	for _, r := range e.pages.Routes() {
		l := strings.ToLower(r.Pattern)
		for _, bad := range []string{"voir", "comme", "view-as", "viewas", "impersonate", "cible", "target", "paire"} {
			if strings.Contains(l, bad) {
				t.Errorf("route suspecte : %s", r.Pattern)
			}
		}
	}
	ck := e.adminCookies(t, e.groupA)
	for _, p := range []string{"voir-comme", "voir_comme", "voir", "as", "cible"} {
		rec := e.get(t, "/noel/admin/participants/"+sid(a)+"/"+p, "fr", ck)
		if rec.Code != 404 && rec.Code != 405 {
			t.Errorf("/%s : 404 ou 405 attendu, %d", p, rec.Code)
		}
	}
}
