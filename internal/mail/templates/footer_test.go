package templates

import (
	"strings"
	"testing"
)

// F6.AC29 : chaque modèle se termine par un pied de message qui explique
// pourquoi la personne reçoit le courriel et qui l'envoie.
func TestFooter(t *testing.T) {
	cat := testCatalog(t)
	for _, k := range Kinds() {
		d := sampleData(k)
		d.AppName = "Santa du groupe"
		m := render(t, cat, k, d)
		i := strings.LastIndex(m.Text, "\n--\n")
		if i < 0 {
			t.Fatalf("%s : pas de pied de message", k)
		}
		foot := m.Text[i:]
		if !strings.Contains(foot, "Tu reçois ce message parce que") {
			t.Errorf("%s : le pied n'explique pas pourquoi :\n%s", k, foot)
		}
		if !strings.Contains(foot, "Santa du groupe") || !strings.Contains(foot, "Famille Martin") || !strings.Contains(foot, "envoyé automatiquement") {
			t.Errorf("%s : le pied ne dit pas qui envoie :\n%s", k, foot)
		}
		hi := strings.LastIndex(m.HTML, "<hr>")
		if hi < 0 || !strings.Contains(m.HTML[hi:], "Tu reçois ce message parce que") {
			t.Errorf("%s : pied HTML absent", k)
		}
		info, _ := InfoOf(k)
		if !info.Essential && !strings.HasSuffix(strings.TrimSpace(foot), d.UnsubscribeURL) {
			t.Errorf("%s : le pied d'un mail non essentiel se termine par le lien de désinscription", k)
		}
	}
}

func TestFooterDefaultAppName(t *testing.T) {
	m := render(t, testCatalog(t), Welcome, sampleData(Welcome))
	if !strings.Contains(m.Text, DefaultAppName) {
		t.Errorf("nom d'application par défaut absent")
	}
}
