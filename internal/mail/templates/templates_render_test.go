package templates

import (
	"strings"
	"testing"
)

// F6.AC10 : chaque modèle est rendu en français, nomme le destinataire ;
// l'invitation contient le lien et le code personnels.
func TestEveryTemplateRendersInFrenchAndNamesTheRecipient(t *testing.T) {
	cat := testCatalog(t)
	for _, k := range Kinds() {
		t.Run(string(k), func(t *testing.T) {
			m, out := buildKind(t, cat, k, sampleData(k))
			if !strings.HasPrefix(m.Text, "Bonjour Léa,") {
				t.Errorf("le texte ne nomme pas le destinataire :\n%s", m.Text)
			}
			if !strings.Contains(m.HTML, "Bonjour Léa,") {
				t.Errorf("le HTML ne nomme pas le destinataire")
			}
			if m.ToName != "Léa" || m.To != "lea@example.net" {
				t.Errorf("destinataire %q <%s>", m.ToName, m.To)
			}
			if strings.Contains(m.Text, "{") || strings.Contains(m.Subject, "{") {
				t.Errorf("paramètre non remplacé :\n%s\n%s", m.Subject, m.Text)
			}
			if !strings.Contains(string(out.Raw), "Content-Language: fr") {
				t.Errorf("Content-Language fr absent")
			}
		})
	}
}

func TestInvitationHasPersonalLinkAndCode(t *testing.T) {
	cat := testCatalog(t)
	m := render(t, cat, Invitation, sampleData(Invitation))
	for _, part := range []string{m.Text, m.HTML} {
		if !strings.Contains(part, personalLink) || !strings.Contains(part, "K7M2QX") {
			t.Errorf("lien ou code personnel absent :\n%s", part)
		}
	}
	if !strings.Contains(m.HTML, `href="`+personalLink+`"`) {
		t.Errorf("le lien n'est pas cliquable")
	}
}

func TestRenderRefusesIncompleteData(t *testing.T) {
	cat := testCatalog(t)
	bad := map[string]func(*Data){
		"sans prénom":  func(d *Data) { d.FirstName = "" },
		"sans groupe":  func(d *Data) { d.GroupName = "" },
		"sans adresse": func(d *Data) { d.To = "" },
		"sans lien":    func(d *Data) { d.Link = "" },
		"sans code":    func(d *Data) { d.Code = "" },
	}
	for name, mod := range bad {
		d := sampleData(Invitation)
		mod(&d)
		if _, err := Render(cat, Invitation, d); err == nil {
			t.Errorf("%s : erreur attendue", name)
		}
	}
	d := sampleData(ReminderLetter)
	d.UnsubscribeURL = ""
	if _, err := Render(cat, ReminderLetter, d); err == nil {
		t.Errorf("un rappel sans lien de désinscription doit être refusé")
	}
	if _, err := Render(cat, Kind("nope"), sampleData(Invitation)); err == nil {
		t.Errorf("modèle inconnu accepté")
	}
}

func TestFormatBudget(t *testing.T) {
	cases := []struct {
		lang  string
		cents int64
		want  string
	}{{"fr", 2500, "25 €"}, {"fr", 2550, "25,50 €"}, {"en", 2500, "€25"}, {"en", 2505, "€25.05"}}
	for _, c := range cases {
		if got := FormatBudget(c.lang, c.cents); got != c.want {
			t.Errorf("%s %d : %q, attendu %q", c.lang, c.cents, got, c.want)
		}
	}
}
