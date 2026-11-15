package settingspages

import (
	"strings"
	"testing"
)

// F10c.AC9 : la langue par défaut parmi les langues chargées, enregistrée par le service F2e.
func TestDefaultLanguageFormListsLoadedLanguagesAndSaves(t *testing.T) {
	e := newEnv(t)
	gid := e.group("langue")
	ck := e.adminCookie(gid)
	page := "/langue/admin/reglages"
	body := e.get(page, "fr", ck).Body.String()
	for _, l := range e.cat.Langs() {
		if !strings.Contains(body, `<option value="`+l.Code+`"`) {
			t.Errorf("langue %s absente de la liste", l.Code)
		}
	}
	if strings.Contains(body, `<option value="de"`) {
		t.Error("seules les langues chargées sont proposées")
	}
	f := groupFields("Groupe langue")
	f.Set("lang", "en")
	if rec := e.post(page+"/groupe", page, ck, f); rec.Code != 303 {
		t.Fatalf("enregistrement : %d", rec.Code)
	}
	if got, ok, _ := e.settings.DefaultLanguage(ctx, gid); !ok || got != "en" {
		t.Fatalf("langue enregistrée : %q %v", got, ok)
	}
	if !strings.Contains(e.get(page, "fr", ck).Body.String(), `<option value="en" selected>`) {
		t.Error("la langue enregistrée doit être sélectionnée")
	}
	f.Set("lang", "zz")
	if rec := e.post(page+"/groupe", page, ck, f); rec.Code != 422 {
		t.Errorf("langue inconnue : %d", rec.Code)
	}
	if got, _, _ := e.settings.DefaultLanguage(ctx, gid); got != "en" {
		t.Error("une langue refusée ne change rien")
	}
	f.Set("lang", "")
	e.post(page+"/groupe", page, ck, f)
	if _, ok, _ := e.settings.DefaultLanguage(ctx, gid); ok {
		t.Error("vide : retour à la langue de l'instance")
	}
}
