package settingspages

import (
	"strings"
	"testing"
)

// F10c.AC5 : plafond de 2 à 5, confirmation explicite au-delà de 2.
func TestMaxGiftsBoundsAndConfirmation(t *testing.T) {
	e := newEnv(t)
	gid := e.group("plafond")
	ck := e.adminCookie(gid)
	page := "/plafond/admin/reglages"
	save := func(v string, confirm bool) int {
		f := groupFields("Groupe plafond")
		f.Set("max_gifts", v)
		if confirm {
			f.Set("confirm_max_gifts", "1")
		}
		rec := e.post(page+"/groupe", page, ck, f)
		if rec.Code == 422 && !strings.Contains(rec.Body.String(), `role="alert"`) {
			t.Errorf("%s : l'erreur doit s'afficher sous le champ", v)
		}
		return rec.Code
	}
	for _, v := range []string{"1", "6", "0", "x", ""} {
		if c := save(v, true); c != 422 {
			t.Errorf("plafond %q : 422 attendu, %d", v, c)
		}
	}
	if e.row(gid).max != 2 {
		t.Fatal("rien ne doit être écrit")
	}
	if c := save("3", false); c != 422 || e.row(gid).max != 2 {
		t.Fatalf("sans confirmation : %d", c)
	}
	if c := save("3", true); c != 303 || e.row(gid).max != 3 {
		t.Fatalf("avec confirmation : %d", c)
	}
	if c := save("5", true); c != 303 || e.row(gid).max != 5 {
		t.Fatalf("plafond 5 : %d", c)
	}
	// Redescendre à 2 n'exige aucune confirmation.
	if c := save("2", false); c != 303 || e.row(gid).max != 2 {
		t.Fatalf("retour à 2 : %d", c)
	}
	// Un plafond inchangé au-delà de 2 ne réclame pas la confirmation à chaque enregistrement.
	save("4", true)
	if c := save("4", false); c != 303 || e.row(gid).max != 4 {
		t.Fatalf("plafond inchangé : %d", c)
	}
	body := e.get(page, "fr", ck).Body.String()
	if !strings.Contains(body, `name="max_gifts"`) || !strings.Contains(body, `name="confirm_max_gifts"`) || !strings.Contains(body, `value="4"`) {
		t.Error("le champ et la confirmation doivent figurer dans la page")
	}
}
