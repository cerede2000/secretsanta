package admin

import (
	"net/url"
	"strings"
	"testing"
)

// F11e.AC3 : réglage « Proposer aux inscrits de choisir leur rôle » désactivé
// par défaut, modifiable à tout moment (mode actif ou non, tirage fait ou non).
func TestRoleChoiceSettingOffByDefaultAndEditable(t *testing.T) {
	e := newEnv(t)
	page := e.do("GET", "/famille/admin/inscriptions", "fr", nil, e.cookies["famille"]).Body.String()
	if !strings.Contains(page, "Proposer aux inscrits de choisir leur rôle") {
		t.Fatal("réglage absent")
	}
	if !strings.Contains(page, `aria-labelledby="regadm-role"`) || !strings.Contains(page, `aria-checked="false"`) || !strings.Contains(page, "Désactivé") {
		t.Error("le réglage doit être désactivé par défaut")
	}
	w := e.post(t, "famille", "/famille/admin/inscriptions/choix-role", url.Values{"enabled": {"1"}})
	if w.Code != 303 || !e.inv.role[1] {
		t.Fatalf("activation : %d %v", w.Code, e.inv.role)
	}
	if e.inv.role[2] {
		t.Error("l'autre groupe ne doit pas changer")
	}
	page = e.do("GET", w.Header().Get("Location"), "fr", nil, e.cookies["famille"]).Body.String()
	if !strings.Contains(page, "Activé") {
		t.Error("l'état activé n'est pas affiché")
	}
	// Mode des inscriptions désactivé : le réglage reste modifiable.
	if w := e.post(t, "famille", "/famille/admin/inscriptions/choix-role", url.Values{"enabled": {"0"}}); w.Code != 303 || e.inv.role[1] {
		t.Fatalf("désactivation : %d %v", w.Code, e.inv.role)
	}
	// Tirage fait : toujours modifiable et visible.
	e.draw.done = true
	page = e.do("GET", "/famille/admin/inscriptions", "fr", nil, e.cookies["famille"]).Body.String()
	if !strings.Contains(page, "Proposer aux inscrits de choisir leur rôle") {
		t.Error("réglage absent après le tirage")
	}
	if w := e.post(t, "famille", "/famille/admin/inscriptions/choix-role", url.Values{"enabled": {"1"}}); w.Code != 303 || !e.inv.role[1] {
		t.Fatalf("après tirage : %d", w.Code)
	}
}
