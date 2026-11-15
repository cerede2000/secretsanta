package mailmsg

import (
	"bytes"
	"io"
	"mime/quotedprintable"
	"strings"
	"testing"
)

// F6b.AC1 : multipart/alternative, texte puis HTML, UTF-8 quoted-printable,
// lignes d'au plus 76 caractères, décodage sans perte.

func decodeQP(t *testing.T, b []byte) string {
	t.Helper()
	out, err := io.ReadAll(quotedprintable.NewReader(bytes.NewReader(b)))
	if err != nil {
		t.Fatalf("quoted-printable invalide : %v", err)
	}
	return strings.ReplaceAll(string(out), "\r\n", "\n")
}

func TestMIMEStructureTextThenHTML(t *testing.T) {
	m := validMessage()
	long := strings.Repeat("Joyeux Noël à toute la famille réunie autour du sapin enneigé, ", 4)
	m.Text = m.Text + long + "\n" + strings.Repeat("é", 120) + "\nligne finale avec espaces   \n"
	m.HTML = m.HTML + "\n<p>" + long + "</p>\n<p>" + strings.Repeat("é", 120) + "</p>\n<p>ligne finale avec espaces</p>"
	out := build(t, newBuilder(t, nil), m)
	msg := parse(t, out.Raw)

	_, parts := rawParts(t, msg)
	if len(parts) != 2 {
		t.Fatalf("%d parties, attendu 2", len(parts))
	}
	wantTypes := []string{"text/plain; charset=UTF-8", "text/html; charset=UTF-8"}
	for i, p := range parts {
		if got := p.header["Content-Type"]; len(got) != 1 || got[0] != wantTypes[i] {
			t.Errorf("partie %d : Content-Type %v, attendu %q", i, got, wantTypes[i])
		}
		if got := p.header["Content-Transfer-Encoding"]; len(got) != 1 || got[0] != "quoted-printable" {
			t.Errorf("partie %d : Content-Transfer-Encoding %v", i, got)
		}
		for _, line := range strings.Split(string(p.body), "\r\n") {
			if len(line) > 76 {
				t.Errorf("partie %d : ligne de %d caractères : %q", i, len(line), line)
			}
		}
	}

	if got := decodeQP(t, parts[0].body); got != m.Text {
		t.Errorf("texte décodé différent :\n%q\nattendu\n%q", got, m.Text)
	}
	gotHTML := decodeQP(t, parts[1].body)
	if !strings.Contains(gotHTML, m.HTML) {
		t.Errorf("HTML décodé ne contient pas le fragment d'origine :\n%s", gotHTML)
	}
	if !strings.HasPrefix(gotHTML, "<!DOCTYPE html>\n<html lang=\"fr\">") || !strings.Contains(gotHTML, `<meta charset="utf-8">`) {
		t.Errorf("document HTML sans doctype, lang fr ou charset :\n%s", gotHTML)
	}
	if err := LintHTML(gotHTML, newBuilder(t, nil).Policy(m)); err != nil {
		t.Errorf("le document HTML produit ne passe pas le contrôle : %v", err)
	}
}

func TestMIMEWireFormat(t *testing.T) {
	m := validMessage()
	m.Text += strings.Repeat("ç", 200) + "\n"
	m.HTML += "<p>" + strings.Repeat("ç", 200) + "</p>"
	out := build(t, newBuilder(t, nil), m)
	raw := out.Raw
	for i, c := range raw {
		if c > 127 {
			t.Fatalf("octet non ASCII %#x à la position %d : le message doit être 7 bits", c, i)
		}
		if c == '\n' && (i == 0 || raw[i-1] != '\r') {
			t.Fatalf("LF nu à la position %d", i)
		}
		if c == '\r' && (i+1 >= len(raw) || raw[i+1] != '\n') {
			t.Fatalf("CR nu à la position %d", i)
		}
	}
	for _, line := range strings.Split(string(raw), "\r\n") {
		if len(line) > 78 {
			t.Errorf("ligne de %d caractères : %q", len(line), line)
		}
	}
	_, body, _ := strings.Cut(string(raw), "\r\n\r\n")
	for _, line := range strings.Split(body, "\r\n") {
		if len(line) > 76 {
			t.Errorf("ligne de corps de %d caractères : %q", len(line), line)
		}
	}
}

func TestMIMEFullHTMLDocumentKept(t *testing.T) {
	m := validMessage()
	m.HTML = "<!DOCTYPE html>\n<html lang=\"fr\"><head><meta charset=\"utf-8\"><title>Tirage</title></head><body>" + m.HTML + "</body></html>"
	out := build(t, newBuilder(t, nil), m)
	_, parts := rawParts(t, parse(t, out.Raw))
	if got := decodeQP(t, parts[1].body); got != m.HTML {
		t.Errorf("document complet modifié :\n%q\nattendu\n%q", got, m.HTML)
	}
}

func TestMIMERequiresBothVersions(t *testing.T) {
	b := newBuilder(t, nil)
	m := validMessage()
	m.HTML = ""
	_, err := b.Build(m)
	assertKind(t, err, ErrBody)
	m = validMessage()
	m.Text = "  \n"
	_, err = b.Build(m)
	assertKind(t, err, ErrBody)
	m = validMessage()
	m.Text += "\xff"
	_, err = b.Build(m)
	assertKind(t, err, ErrBody)
}
