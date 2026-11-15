package maildiagpages_test

import (
	"regexp"
	"strings"
	"testing"
)

// F6c.AC12 : chaque point à corriger a un bouton Copier, sans boîte du navigateur.
func TestEachRecordHasCopyButton(t *testing.T) {
	e := newEnv(t)
	body := e.get(pagePath, "").Body.String()
	buttons := regexp.MustCompile(`<button[^>]*data-copy-target="([^"]+)"[^>]*>`).FindAllStringSubmatch(body, -1)
	if len(buttons) != 1 {
		t.Fatalf("%d bouton(s) Copier, 1 attendu", len(buttons))
	}
	if !strings.Contains(buttons[0][0], `type="button"`) || !strings.Contains(buttons[0][0], `data-copied="`) || !strings.Contains(buttons[0][0], `data-failed="`) {
		t.Errorf("bouton : %s", buttons[0][0])
	}
	target := regexp.MustCompile(`id="` + regexp.QuoteMeta(buttons[0][1]) + `"[^>]*>([^<]*)<`).FindStringSubmatch(body)
	if target == nil || !strings.Contains(target[1], "_dmarc.exemple.fr TXT") || !strings.Contains(target[1], "v=DMARC1") {
		t.Errorf("texte à copier : %v", target)
	}
	if !strings.Contains(body, `src="/static/js/groups/copy.js"`) {
		t.Error("script de copie absent")
	}
	for _, banned := range []string{"alert(", "confirm(", "prompt("} {
		if strings.Contains(body, banned) {
			t.Errorf("boîte du navigateur : %s", banned)
		}
	}
	// Le point conforme et le point sans objet n'ont pas de bouton.
	if strings.Count(body, "data-copy-target=") != 1 {
		t.Error("bouton Copier sur un point sans enregistrement")
	}
}
