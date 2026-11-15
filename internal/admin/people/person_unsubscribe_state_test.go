package people

import (
	"context"
	"strings"
	"testing"

	"secretsanta/internal/participants"
)

func TestSheetShowsUnsubscribeStatePerMailType(t *testing.T) {
	e := newEnv(t)
	a := e.person(t, e.groupA, "Alice", "alice@exemple.fr", participants.RoleNormal)
	b := e.person(t, e.groupA, "Bruno", "bruno@exemple.fr", participants.RoleNormal)
	e.seedDraw(t, e.groupA, []int64{a, b})
	if err := e.unsub.Unsubscribe(context.Background(), e.groupA, a, "relances"); err != nil {
		t.Fatal(err)
	}
	ck := e.adminCookies(t, e.groupA)
	body := e.get(t, "/noel/admin/participants/"+sid(a), "fr", ck).Body.String()
	i := strings.Index(body, "Relances")
	j := strings.Index(body, "Nouvelle idée")
	if i < 0 || j < 0 || i > j {
		t.Fatal("les deux types doivent être listés : relances puis nouvelle idée")
	}
	if !strings.Contains(body[i:j], "Désinscrit") {
		t.Error("les relances devraient être signalées désinscrites")
	}
	if strings.Contains(body[j:strings.Index(body[j:], "Mails essentiels")+j], "Désinscrit") {
		t.Error("« nouvelle idée » ne devrait pas être désinscrite")
	}
	if !strings.Contains(body, "ne sont pas désactivables") || !strings.Contains(body, "Toujours envoyés") {
		t.Error("les mails essentiels doivent être signalés non désactivables")
	}
	for _, leak := range []string{"Bruno", "bruno@exemple.fr"} {
		if strings.Contains(body, leak) {
			t.Errorf("la fiche contient %q", leak)
		}
	}
	// Aucune écriture possible depuis la fiche sur les mails essentiels : pas de route de désinscription ici.
	for _, r := range e.pages.Routes() {
		if strings.Contains(r.Pattern, "desinscri") {
			t.Errorf("route de désinscription inattendue : %s", r.Pattern)
		}
	}
	other := e.get(t, "/noel/admin/participants/"+sid(b), "fr", ck).Body.String()
	if strings.Contains(other, "Désinscrit") {
		t.Error("l'état d'Alice ne doit pas déteindre sur Bruno")
	}
}
