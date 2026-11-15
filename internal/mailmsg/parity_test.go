package mailmsg

import (
	"errors"
	"slices"
	"testing"
)

// F6b.AC7 : ParityCheck signale tout lien ou toute information présente dans
// le HTML et absente du texte, et réciproquement.

func parityErr(t *testing.T, text, html string) *ParityError {
	t.Helper()
	err := ParityCheck(text, html)
	if err == nil {
		t.Fatalf("écart attendu, aucun")
	}
	if !errors.Is(err, ErrParity) {
		t.Fatalf("erreur %v hors de la catégorie ErrParity", err)
	}
	var pe *ParityError
	if !errors.As(err, &pe) {
		t.Fatalf("erreur %T, attendu *ParityError", err)
	}
	return pe
}

func TestParityEquivalentVersions(t *testing.T) {
	m := validMessage()
	if err := ParityCheck(m.Text, m.HTML); err != nil {
		t.Errorf("versions équivalentes signalées : %v", err)
	}
	// Lien porté par une ancre au texte libellé, adresse en mailto, image
	// décorative, mise en forme en ligne, entités HTML.
	text := "Bonjour Léa,\nVoir mon tirage : https://noel.example.org/x?a=1&b=2\nÉcrire : desinscription@example.org\n"
	html := `<p>Bonjour <strong>Lé</strong>a,</p><p><img src="` + pngData + `" alt="Sapin décoratif"></p>` +
		`<p><a href="https://noel.example.org/x?a=1&amp;b=2">Voir mon tirage</a></p>` +
		`<p>&Eacute;crire : <a href="mailto:Desinscription@example.org?subject=stop">desinscription@example.org</a></p>`
	if err := ParityCheck(text, html); err != nil {
		t.Errorf("versions équivalentes signalées : %v", err)
	}
}

func TestParityLinkOnlyInHTML(t *testing.T) {
	pe := parityErr(t, "Bonjour Léa, voir mon tirage.",
		`<p>Bonjour Léa, <a href="https://noel.example.org/tirage">voir mon tirage</a>.</p>`)
	if !slices.Equal(pe.LinksMissingInText, []string{"https://noel.example.org/tirage"}) {
		t.Errorf("LinksMissingInText = %v", pe.LinksMissingInText)
	}
	if len(pe.LinksMissingInHTML)+len(pe.WordsMissingInText)+len(pe.WordsMissingInHTML) != 0 {
		t.Errorf("écarts en trop : %+v", pe)
	}
}

func TestParityLinkOnlyInText(t *testing.T) {
	pe := parityErr(t, "Bonjour Léa, voir mon tirage : https://noel.example.org/tirage",
		`<p>Bonjour Léa, voir mon tirage :</p>`)
	if !slices.Equal(pe.LinksMissingInHTML, []string{"https://noel.example.org/tirage"}) {
		t.Errorf("LinksMissingInHTML = %v", pe.LinksMissingInHTML)
	}
}

func TestParityDifferentLinkTargets(t *testing.T) {
	pe := parityErr(t, "Voir : https://noel.example.org/a",
		`<p>Voir : <a href="https://noel.example.org/b">https://noel.example.org/a</a></p>`)
	if !slices.Equal(pe.LinksMissingInText, []string{"https://noel.example.org/b"}) {
		t.Errorf("LinksMissingInText = %v", pe.LinksMissingInText)
	}
}

func TestParityInformationOnlyInHTML(t *testing.T) {
	pe := parityErr(t, "Le tirage est fait.\nBudget : 30 €.",
		`<p>Le tirage est fait.</p><p>Budget : 30 €, avant le 24 décembre.</p>`)
	// « le » figure déjà dans le texte : seul ce qui manque est signalé.
	want := []string{"24", "avant", "décembre"}
	if !slices.Equal(pe.WordsMissingInText, want) {
		t.Errorf("WordsMissingInText = %v, attendu %v", pe.WordsMissingInText, want)
	}
}

func TestParityInformationOnlyInText(t *testing.T) {
	pe := parityErr(t, "Le tirage est fait. Code : 4821.",
		`<p>Le tirage est fait.</p>`)
	if !slices.Equal(pe.WordsMissingInHTML, []string{"4821", "code"}) {
		t.Errorf("WordsMissingInHTML = %v", pe.WordsMissingInHTML)
	}
	if pe.Error() == "" {
		t.Errorf("message vide")
	}
}

func TestParityIgnoresHeadAndStyle(t *testing.T) {
	html := `<!DOCTYPE html><html lang="fr"><head><title>Titre caché</title><style>p{color:red}</style></head><body><p>Bonjour</p></body></html>`
	if err := ParityCheck("Bonjour", html); err != nil {
		t.Errorf("head ou style comptés comme information : %v", err)
	}
}

func TestBuildRefusesNonEquivalentVersions(t *testing.T) {
	m := validMessage()
	m.HTML += "<p>Rendez-vous le 24 décembre.</p>"
	_, err := newBuilder(t, nil).Build(m)
	assertKind(t, err, ErrParity)
}
