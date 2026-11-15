package public

import (
	"net/http"
	"strings"
	"testing"
	"time"
)

func successBody(t *testing.T, e *env, lang string) string {
	t.Helper()
	r := e.do(t, http.MethodPost, "/noel/inscription/code", codeForm("ref-1", "ABC123"), lang)
	if r.Code != http.StatusOK {
		t.Fatalf("réussite : %d", r.Code)
	}
	return r.Body.String()
}

// F11c.AC2 : sans budget, aucune mention ni espace réservé ; avec budget, le
// montant et la date de révélation sont rappelés.
func TestSuccessHidesBudgetWhenEmpty(t *testing.T) {
	e := newEnv(t)
	for _, lang := range []string{"fr", "en"} {
		body := successBody(t, e, lang)
		for _, w := range []string{"Budget", "budget", "€", "Découverte du tirage", "Draw reveal", "reg-budget"} {
			if strings.Contains(body, w) {
				t.Errorf("%s : %q présent sans budget", lang, w)
			}
		}
		// Le reste de l'écran est là : lien personnel et lettre.
		if !strings.Contains(body, "reg-letter") || !strings.Contains(body, "reg-row") {
			t.Errorf("%s : écran incomplet", lang)
		}
		if strings.Count(body, `class="reg-row"`) != 1 {
			t.Errorf("%s : seule la ligne du lien personnel est attendue, sans espace réservé", lang)
		}
	}
}

func TestSuccessShowsBudgetAndRevealDateWhenDefined(t *testing.T) {
	e := newEnv(t)
	e.group.BudgetCents = cents(2500)
	e.group.RevealAt = at(2026, time.December, 1, 18)
	body := successBody(t, e, "fr")
	for _, w := range []string{"Budget : " + e.group.BudgetLabel(), "Découverte du tirage", "1er décembre à 18:00."} {
		if !strings.Contains(body, w) {
			t.Errorf("fr : %q absent", w)
		}
	}
	if strings.Count(body, `class="reg-row"`) != 3 {
		t.Error("fr : trois lignes attendues")
	}
	en := successBody(t, e, "en")
	for _, w := range []string{"Budget: " + e.group.BudgetLabel(), "Draw reveal", "December 1st at 18:00."} {
		if !strings.Contains(en, w) {
			t.Errorf("en : %q absent", w)
		}
	}
}

func TestSuccessBudgetAndRevealAreIndependent(t *testing.T) {
	e := newEnv(t)
	e.group.RevealAt = at(2026, time.December, 24, 20)
	body := successBody(t, e, "fr")
	if strings.Contains(body, "Budget") {
		t.Error("budget mentionné alors qu'il est vide")
	}
	if !strings.Contains(body, "24 décembre à 20:00.") {
		t.Error("date de révélation absente")
	}
	e = newEnv(t)
	e.group.BudgetCents = cents(1250)
	body = successBody(t, e, "fr")
	if !strings.Contains(body, "Budget : "+e.group.BudgetLabel()) || strings.Contains(body, "Découverte") {
		t.Error("budget seul : affichage incorrect")
	}
}
