package templates

import (
	"net/mail"
	"strings"
	"testing"
)

// F6.AC27 : en-têtes complets ; List-Unsubscribe et List-Unsubscribe-Post
// seulement sur les modèles non essentiels.
func TestHeaders(t *testing.T) {
	cat := testCatalog(t)
	for _, k := range Kinds() {
		d := sampleData(k)
		m, out := buildKind(t, cat, k, d)
		h := parseRaw(t, out.Raw).Header

		from, err := mail.ParseAddress(h.Get("From"))
		if err != nil || from.Name != "Père Noël secret" || from.Address != testFrom {
			t.Errorf("%s : From %q", k, h.Get("From"))
		}
		if out.EnvelopeFrom != testFrom {
			t.Errorf("%s : expéditeur d'enveloppe %q", k, out.EnvelopeFrom)
		}
		if _, err := h.Date(); err != nil {
			t.Errorf("%s : Date : %v", k, err)
		}
		if id := h.Get("Message-ID"); !strings.HasSuffix(id, "@example.org>") {
			t.Errorf("%s : Message-ID %q", k, id)
		}
		for name, want := range map[string]string{
			"MIME-Version": "1.0", "Content-Language": "fr", "Auto-Submitted": "auto-generated",
			"X-Auto-Response-Suppress": "All",
		} {
			if h.Get(name) != want {
				t.Errorf("%s : %s = %q, attendu %q", k, name, h.Get(name), want)
			}
		}
		info, _ := InfoOf(k)
		if info.Essential {
			if h.Get("List-Unsubscribe") != "" || h.Get("List-Unsubscribe-Post") != "" || m.UnsubscribeURL != "" {
				t.Errorf("%s : essentiel mais porte une désinscription", k)
			}
			continue
		}
		if h.Get("List-Unsubscribe") != "<"+d.UnsubscribeURL+">" || h.Get("List-Unsubscribe-Post") != "List-Unsubscribe=One-Click" {
			t.Errorf("%s : List-Unsubscribe %q / %q", k, h.Get("List-Unsubscribe"), h.Get("List-Unsubscribe-Post"))
		}
	}
}

func TestEssentialSplit(t *testing.T) {
	nonEssential := map[Kind]string{
		LetterUpdated: UnsubNouvelleIdee, ReminderParticipant: UnsubRelances,
		ReminderLetter: UnsubRelances, ReminderReading: UnsubRelances,
	}
	for _, k := range Kinds() {
		info, _ := InfoOf(k)
		typ, non := nonEssential[k]
		if info.Essential == non || info.UnsubType != typ {
			t.Errorf("%s : %+v", k, info)
		}
	}
}
