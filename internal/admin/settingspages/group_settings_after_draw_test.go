package settingspages

import (
	"net/url"
	"strings"
	"testing"
	"time"
)

// F10c.AC3 : budget vide, défini ou retiré après le tirage ; nom, jour J et consigne ;
// slug en lecture seule ; envoi demandé seulement si l'interrupteur est coché ; aucune paire touchée.
func TestGroupSettingsAfterDrawKeepPairsAndNotifyOnlyWhenAsked(t *testing.T) {
	e := newEnv(t)
	gid := e.group("apres")
	e.addN(gid, "Hortensia", 4)
	e.doDraw(gid)
	before := e.pairs(gid)
	if before == "" {
		t.Fatal("le tirage doit avoir des paires")
	}
	ck := e.adminCookie(gid)
	page := "/apres/admin/reglages"

	body := e.get(page, "fr", ck).Body.String()
	if !strings.Contains(body, `id="stg-slug">apres<`) || strings.Contains(body, `name="slug"`) {
		t.Error("le slug doit s'afficher en lecture seule, sans champ")
	}
	if !strings.Contains(body, `<details class="stg-acc" id="groupe" open>`) || strings.Contains(body, `id="courrier" open`) {
		t.Error("seule la première section est ouverte")
	}

	// Budget défini, sans prévenance.
	f := groupFields("Nouveau nom")
	f.Set("budget", "25,50")
	f.Set("event_date", "2026-12-24")
	f.Set("instructions", "Emballage soigné")
	if rec := e.post(page+"/groupe", page, ck, f); rec.Code != 303 {
		t.Fatalf("enregistrement : %d %s", rec.Code, rec.Body.String())
	}
	r := e.row(gid)
	if r.name != "Nouveau nom" || r.budget.Int64 != 2550 || !r.budget.Valid || r.date.String != "2026-12-24" || r.instr != "Emballage soigné" || r.version != 1 {
		t.Fatalf("réglages : %+v", r)
	}
	if len(e.bus.Emitted) != 1 || e.bus.Emitted[0].Payload["notify"] != "false" {
		t.Fatalf("événement sans prévenance attendu : %+v", e.bus.Emitted)
	}
	if e.pairs(gid) != before {
		t.Fatal("les paires ont changé")
	}

	// Budget changé avec l'interrupteur coché : l'envoi est demandé.
	f.Set("budget", "30")
	f.Set("notify", "1")
	if rec := e.post(page+"/groupe", page, ck, f); rec.Code != 303 {
		t.Fatalf("budget changé : %d", rec.Code)
	}
	if n := len(e.bus.Emitted); n != 2 || e.bus.Emitted[1].Payload["notify"] != "true" {
		t.Fatalf("envoi demandé attendu : %+v", e.bus.Emitted)
	}

	// Budget retiré.
	f.Set("budget", "")
	f.Del("notify")
	if rec := e.post(page+"/groupe", page, ck, f); rec.Code != 303 {
		t.Fatalf("budget retiré : %d", rec.Code)
	}
	if r := e.row(gid); r.budget.Valid {
		t.Fatal("le budget doit être retiré")
	}
	// Un budget inchangé n'émet rien.
	n := len(e.bus.Emitted)
	e.post(page+"/groupe", page, ck, f)
	if len(e.bus.Emitted) != n {
		t.Error("un budget inchangé ne doit rien émettre")
	}

	// Un budget invalide est refusé sous le champ, rien n'est écrit.
	f.Set("budget", "beaucoup")
	rec := e.post(page+"/groupe", page, ck, f)
	if rec.Code != 422 || !strings.Contains(rec.Body.String(), `aria-invalid="true"`) || !strings.Contains(rec.Body.String(), `id="stg-budget-err"`) {
		t.Fatalf("budget invalide : %d", rec.Code)
	}
	// Le slug ne peut pas être changé par la saisie.
	f.Set("budget", "")
	f.Set("slug", "autre")
	e.post(page+"/groupe", page, ck, f)
	if countRows(t, e.db, `SELECT COUNT(*) FROM groups WHERE slug = 'apres'`) != 1 {
		t.Fatal("le slug est immuable")
	}
	if e.pairs(gid) != before {
		t.Fatal("les paires ont changé")
	}
}

func TestRevealDateIsSavedThroughRevealService(t *testing.T) {
	e := newEnv(t)
	gid := e.group("revel")
	ck := e.adminCookie(gid)
	page := "/revel/admin/reglages"
	f := groupFields("Groupe revel")
	f.Set("reveal", "2026-12-01T18:00")
	if rec := e.post(page+"/groupe", page, ck, f); rec.Code != 303 || e.reveal.at == nil ||
		!e.reveal.at.Equal(time.Date(2026, 12, 1, 18, 0, 0, 0, time.UTC)) {
		t.Fatalf("date de révélation : %d %v", rec.Code, e.reveal.at)
	}
	if !strings.Contains(e.get(page, "fr", ck).Body.String(), `value="2026-12-01T18:00"`) {
		t.Error("la date doit être préremplie")
	}
	calls := e.reveal.setCalls
	e.post(page+"/groupe", page, ck, f)
	if e.reveal.setCalls != calls {
		t.Error("une date inchangée n'appelle pas le service")
	}
	f.Set("reveal", "pas une date")
	if rec := e.post(page+"/groupe", page, ck, f); rec.Code != 422 {
		t.Errorf("date invalide : %d", rec.Code)
	}
	e.reveal.locked = true
	if body := e.get(page, "fr", ck).Body.String(); strings.Contains(body, `name="reveal"`) {
		t.Error("date verrouillée : pas de champ")
	}
	_ = url.Values{}
}
