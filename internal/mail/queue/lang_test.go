package queue_test

import (
	"strings"
	"testing"
	"time"
)

// La langue de l'élément (Lang) est conservée en file et fixe Content-Language
// et la balise <html lang> du message envoyé ; vide, le message reste en
// français (comportement d'origine).
func TestItemLangSetsContentLanguage(t *testing.T) {
	e := newEnv(t, nil)
	en := item("amy@example.net", "Amy", 1)
	en.Lang = "en"
	mustEnqueue(t, e, en)
	mustEnqueue(t, e, item("lea@example.net", "Léa", 2))
	// Le dernier message regroupé remplace le précédent, langue comprise.
	first := item("max@example.net", "Max", 3)
	first.CoalesceKey, first.Lang = "k", "fr"
	mustEnqueue(t, e, first)
	second := first
	second.Lang = "en"
	mustEnqueue(t, e, second)

	if n := tick(t, e); n != 2 {
		t.Fatalf("%d envoi(s), attendu 2 (le regroupement attend son délai)", n)
	}
	e.Clk.Advance(time.Hour)
	if n := tick(t, e); n != 1 {
		t.Fatalf("%d envoi(s) après le délai de regroupement, attendu 1", n)
	}
	want := map[string]string{"amy@example.net": "en", "lea@example.net": "fr", "max@example.net": "en"}
	if len(e.Tr.Sent) != 3 {
		t.Fatalf("%d messages envoyés", len(e.Tr.Sent))
	}
	for _, env := range e.Tr.Sent {
		raw := string(env.Raw)
		to := env.To
		lang := want[to]
		if !strings.Contains(raw, "Content-Language: "+lang+"\r\n") {
			t.Errorf("%s : Content-Language %q attendu\n%.600s", to, lang, raw)
		}
		if !strings.Contains(raw, `lang=3D"`+lang+`"`) { // corps encodé en quoted-printable
			t.Errorf("%s : balise <html lang=%q> absente", to, lang)
		}
	}
}
