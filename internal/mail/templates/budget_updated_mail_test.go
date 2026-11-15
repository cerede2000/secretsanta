package templates

import (
	"strings"
	"testing"
)

// F6.AC15 (part du contenu) : le courriel « budget mis à jour » nomme le
// destinataire et indique le nouveau montant ou l'absence de budget ; il est
// essentiel et sans lien de désinscription. Un courriel par participant : deux
// participants qui partagent une adresse reçoivent deux messages distincts.
// L'abonnement à budget_modifie et la mise en file relèvent de la file d'envoi
// et de l'orchestrateur ; ce test couvre le modèle qu'ils appellent.
func TestBudgetUpdatedMail(t *testing.T) {
	cat := testCatalog(t)

	info, ok := InfoOf(BudgetUpdated)
	if !ok || !info.Essential || info.UnsubType != "" {
		t.Fatalf("budget_updated doit être essentiel : %+v", info)
	}

	d := sampleData(BudgetUpdated)
	d.BudgetCents = budget(3000)
	m, out := buildKind(t, cat, BudgetUpdated, d)
	for _, part := range []string{m.Text, m.HTML} {
		if !strings.Contains(part, "Bonjour Léa,") || !strings.Contains(part, "30 €") {
			t.Errorf("destinataire ou montant absent :\n%s", part)
		}
	}
	if !m.Essential || m.UnsubscribeURL != "" {
		t.Errorf("mail essentiel attendu")
	}
	raw := string(out.Raw)
	if strings.Contains(raw, "List-Unsubscribe") || strings.Contains(m.Text, "desinscription") {
		t.Errorf("un mail essentiel ne porte aucune désinscription")
	}

	d.BudgetCents = nil
	m = render(t, cat, BudgetUpdated, d)
	if !strings.Contains(m.Text, "Il n'y a plus de budget fixé") || strings.Contains(m.Text, "€") {
		t.Errorf("l'absence de budget doit être dite :\n%s", m.Text)
	}
	buildKind(t, cat, BudgetUpdated, d)

	// Adresse partagée : un message par participant, chacun nommant le sien.
	a, b := sampleData(BudgetUpdated), sampleData(BudgetUpdated)
	b.FirstName = "Hugo"
	ma, mb := render(t, cat, BudgetUpdated, a), render(t, cat, BudgetUpdated, b)
	if ma.To != mb.To || ma.Text == mb.Text {
		t.Errorf("deux messages distincts attendus pour une adresse partagée")
	}
	if strings.Contains(ma.Text, "Hugo") || strings.Contains(mb.Text, "Léa") {
		t.Errorf("un message nomme l'autre participant")
	}
}
