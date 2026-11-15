package accesspages

import (
	"fmt"
	"net/http"
	"testing"
	"time"
)

// failAcrossAddresses enregistre n échecs répartis sur de nombreuses adresses
// (chacune reste sous sa propre limite).
func failAcrossAddresses(t *testing.T, e *env, n int) {
	t.Helper()
	for i := 0; i < n; i++ {
		ip := fmt.Sprintf("203.0.113.%d", 1+i/2)
		if rec := e.do(t, http.MethodPost, "/noel/admin/connexion", adminForm("faux"), ip, "fr"); rec.Code != http.StatusUnauthorized {
			t.Fatalf("échec %d : %d", i, rec.Code)
		}
	}
}

// Le seau du groupe (AdminGroupAttempts) borne les devinettes réparties sur
// beaucoup d'adresses : 49 échecs ne bloquent rien, le 50e ferme l'accès
// jusqu'à la fin de la fenêtre, même pour le bon secret d'une adresse neuve.
func TestAdminGroupBucketBlocksSpreadGuesses(t *testing.T) {
	if AdminGroupAttempts.Limit != 50 {
		t.Fatalf("limite attendue 50, %d", AdminGroupAttempts.Limit)
	}
	t.Run("49 échecs ne bloquent pas", func(t *testing.T) {
		e := newEnv(t)
		g := e.createGroup(t, "noel")
		failAcrossAddresses(t, e, AdminGroupAttempts.Limit-1)
		if rec := e.do(t, http.MethodPost, "/noel/admin/connexion", adminForm(g.AdminSecret), "198.51.100.77", "fr"); rec.Code != http.StatusSeeOther {
			t.Fatalf("303 attendu sous la limite : %d", rec.Code)
		}
	})
	t.Run("50 échecs bloquent jusqu'à la fin de la fenêtre", func(t *testing.T) {
		e := newEnv(t)
		g := e.createGroup(t, "noel")
		failAcrossAddresses(t, e, AdminGroupAttempts.Limit)
		if rec := e.do(t, http.MethodPost, "/noel/admin/connexion", adminForm(g.AdminSecret), "198.51.100.77", "fr"); rec.Code != http.StatusTooManyRequests {
			t.Fatalf("429 attendu : %d", rec.Code)
		}
		if e.sessions.count() != 0 {
			t.Fatal("aucune session pendant le blocage")
		}
		e.clock.Advance(AdminGroupAttempts.Window + time.Second)
		if rec := e.do(t, http.MethodPost, "/noel/admin/connexion", adminForm(g.AdminSecret), "198.51.100.78", "fr"); rec.Code != http.StatusSeeOther {
			t.Fatalf("la fenêtre écoulée doit rouvrir l'accès : %d", rec.Code)
		}
	})
}
