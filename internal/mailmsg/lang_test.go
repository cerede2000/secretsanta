package mailmsg

import (
	"strings"
	"testing"
)

// englishMessage est un courriel anglais valide (fragment HTML).
func englishMessage() Message {
	return Message{
		To:      testTo,
		ToName:  "Lea",
		Subject: "The draw is done",
		Text:    "Hello Lea,\n\nThe draw is done. Find out who you give a gift to:\n" + testLink + "\n",
		HTML: "<p>Hello Lea,</p>\n<p>The draw is done. Find out who you give a gift to:</p>\n" +
			`<p><a href="` + testLink + `">` + testLink + "</a></p>",
		Essential: true,
		Lang:      "en",
	}
}

func htmlPart(t *testing.T, raw []byte) string {
	t.Helper()
	_, parts := rawParts(t, parse(t, raw))
	return decodeQP(t, parts[1].body)
}

// Un courriel anglais porte Content-Language: en et <html lang="en">.
func TestLangEnglishHeaderAndHTMLTag(t *testing.T) {
	out := build(t, newBuilder(t, nil), englishMessage())
	if got := parse(t, out.Raw).Header["Content-Language"]; len(got) != 1 || got[0] != "en" {
		t.Errorf("Content-Language = %v, attendu [en]", got)
	}
	doc := htmlPart(t, out.Raw)
	if !strings.HasPrefix(doc, "<!DOCTYPE html>\n<html lang=\"en\">") {
		t.Errorf("balise html : %.60q", doc)
	}
	if strings.Contains(doc, `lang="fr"`) {
		t.Errorf("lang fr résiduel dans le document anglais")
	}
	if err := LintHTML(doc, Policy{PublicHost: "noel.example.org", Lang: "en"}); err != nil {
		t.Errorf("le document produit ne passe pas le contrôle : %v", err)
	}
}

// Sans Lang, le comportement d'avant : fr.
func TestLangEmptyDefaultsToFrench(t *testing.T) {
	out := build(t, newBuilder(t, nil), validMessage())
	if got := parse(t, out.Raw).Header["Content-Language"]; len(got) != 1 || got[0] != "fr" {
		t.Errorf("Content-Language = %v, attendu [fr]", got)
	}
	if doc := htmlPart(t, out.Raw); !strings.HasPrefix(doc, "<!DOCTYPE html>\n<html lang=\"fr\">") {
		t.Errorf("balise html : %.60q", doc)
	}
}

func TestLangRegionalTagAccepted(t *testing.T) {
	m := englishMessage()
	m.Lang = "pt-BR"
	out := build(t, newBuilder(t, nil), m)
	if got := parse(t, out.Raw).Header.Get("Content-Language"); got != "pt-BR" {
		t.Errorf("Content-Language = %q", got)
	}
	if doc := htmlPart(t, out.Raw); !strings.Contains(doc, `<html lang="pt-BR">`) {
		t.Errorf("balise html : %.60q", doc)
	}
}

// Un code invalide est refusé : il finirait dans un en-tête.
func TestLangInvalidRefused(t *testing.T) {
	for _, bad := range []string{"e", "english", "fr_FR", "fr\r\nBcc: x@example.org", "en-", "-en", "fr fr", "en-x", "12", `en"><script`} {
		m := englishMessage()
		m.Lang = bad
		_, err := newBuilder(t, nil).Build(m)
		assertKind(t, err, ErrLang)
	}
}

// Un document complet doit déclarer la langue du message.
func TestLangFullDocumentMustMatch(t *testing.T) {
	doc := func(lang string) string {
		return "<!DOCTYPE html>\n<html lang=\"" + lang + "\"><head><meta charset=\"utf-8\"><title>T</title></head><body>" +
			englishMessage().HTML + "</body></html>"
	}
	b := newBuilder(t, nil)

	m := englishMessage()
	m.HTML = doc("en")
	out := build(t, b, m)
	if got := htmlPart(t, out.Raw); got != m.HTML {
		t.Errorf("le document complet doit rester tel quel")
	}
	m.HTML = doc("en-GB")
	build(t, b, m)

	m.HTML = doc("fr")
	_, err := b.Build(m)
	assertKind(t, err, ErrHTML)

	// Sans Lang, un document anglais est refusé comme avant.
	m = englishMessage()
	m.Lang = ""
	m.HTML = doc("en")
	_, err = b.Build(m)
	assertKind(t, err, ErrHTML)
}

func TestLintHTMLLangPolicy(t *testing.T) {
	src := `<!DOCTYPE html><html lang="en"><body><p>x</p></body></html>`
	if err := LintHTML(src, Policy{PublicHost: "noel.example.org", Lang: "en"}); err != nil {
		t.Errorf("lang en accepté attendu : %v", err)
	}
	assertKind(t, LintHTML(src, Policy{PublicHost: "noel.example.org"}), ErrHTML)
	assertKind(t, LintHTML(`<html lang="english"><body><p>x</p></body></html>`, Policy{PublicHost: "noel.example.org", Lang: "en"}), ErrHTML)
}
