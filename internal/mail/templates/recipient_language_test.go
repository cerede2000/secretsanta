package templates

import (
	"io/fs"
	"log/slog"
	"strings"
	"testing"
	"testing/fstest"

	"secretsanta/internal/i18n/catalog"
)

// F6.AC37 : langue du participant, sinon du groupe, sinon par défaut de
// l'instance ; repli sur la langue par défaut pour une clé manquante.
func TestRecipientLanguage(t *testing.T) {
	cat := testCatalog(t)
	cases := []struct {
		participant, group, want string
	}{
		{"en", "fr", "en"},
		{"", "en", "en"},
		{"", "", "fr"},
		{"xx", "en", "en"},
		{"xx", "yy", "fr"},
		{"en-GB", "", "en"},
		{"fr", "en", "fr"},
	}
	for _, c := range cases {
		if got := ResolveLang(cat, c.participant, c.group); got != c.want {
			t.Errorf("participant %q groupe %q : %q, attendu %q", c.participant, c.group, got, c.want)
		}
	}

	d := sampleData(Welcome)
	d.ParticipantLang, d.GroupLang = "", "en"
	if m := render(t, cat, Welcome, d); !strings.HasPrefix(m.Text, "Hello Léa,") {
		t.Errorf("langue du groupe non suivie : %q", m.Text)
	}
	d.ParticipantLang = "fr"
	if m := render(t, cat, Welcome, d); !strings.HasPrefix(m.Text, "Bonjour Léa,") {
		t.Errorf("la langue du participant doit primer : %q", m.Text)
	}
	d.ParticipantLang, d.GroupLang = "", ""
	if m := render(t, cat, Welcome, d); !strings.HasPrefix(m.Text, "Bonjour Léa,") {
		t.Errorf("langue par défaut non utilisée : %q", m.Text)
	}
}

// Une clé absente de la langue du destinataire retombe sur la langue par
// défaut : le courriel reste complet et valide.
func TestMissingKeyFallsBackToDefaultLanguage(t *testing.T) {
	full := testCatalog(t)
	fsys := fstest.MapFS{}
	for _, lang := range []string{"fr", "en"} {
		msgs := full.Messages(lang)
		var b strings.Builder
		b.WriteString("{")
		first := true
		for k, v := range msgs {
			if !strings.HasPrefix(k, prefix) {
				continue
			}
			if lang == "en" && k == prefix+"welcome.letter" {
				continue // clé manquante en anglais
			}
			if !first {
				b.WriteString(",")
			}
			first = false
			b.WriteString(jsonString(k) + ":" + jsonString(v))
		}
		b.WriteString("}")
		fsys["static/i18n/mail/templates/"+lang+".json"] = &fstest.MapFile{Data: []byte(b.String())}
	}
	var _ fs.FS = fsys
	cat, err := catalog.Load(fsys, catalog.Options{DefaultLang: "fr", Logger: slog.New(slog.DiscardHandler)})
	if err != nil {
		t.Fatal(err)
	}
	d := sampleData(Welcome)
	d.ParticipantLang = "en"
	m, _ := buildKind(t, cat, Welcome, d)
	if !strings.HasPrefix(m.Text, "Hello Léa,") {
		t.Errorf("le reste du courriel doit rester en anglais :\n%s", m.Text)
	}
	if !strings.Contains(m.Text, "Prends un moment pour écrire ta lettre") {
		t.Errorf("repli sur le français attendu pour la clé manquante :\n%s", m.Text)
	}
}

func jsonString(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range s {
		switch r {
		case '"':
			b.WriteString(`\"`)
		case '\\':
			b.WriteString(`\\`)
		case '\n':
			b.WriteString(`\n`)
		default:
			b.WriteRune(r)
		}
	}
	b.WriteByte('"')
	return b.String()
}
