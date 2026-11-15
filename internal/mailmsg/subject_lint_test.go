package mailmsg

import (
	"mime"
	"strings"
	"testing"
)

// F6b.AC3 : objet encodé RFC 2047, refus au-delà de 60 caractères, d'un mot
// en majuscules intégrales ou de « !!! ».

func rawSubject(t *testing.T, raw []byte) string {
	t.Helper()
	var lines []string
	in := false
	for _, l := range headerLines(raw) {
		switch {
		case strings.HasPrefix(l, "Subject:"):
			in = true
			lines = append(lines, l)
		case in && strings.HasPrefix(l, " "):
			lines = append(lines, l)
		default:
			in = false
		}
	}
	return strings.Join(lines, "\r\n")
}

func TestSubjectEncodedRFC2047(t *testing.T) {
	dec := new(mime.WordDecoder)
	for _, s := range []string{
		"Le tirage de Noël est fait",
		"Ton tirage : découvre à qui tu offres un cadeau cette année",
		"Rappel du tirage",
		"=?utf-8?q?pi=C3=A8ge?= littéral",
		"Prix : 30 € et « idées » ½",
	} {
		m := validMessage()
		m.Subject = s
		out := build(t, newBuilder(t, nil), m)
		raw := rawSubject(t, out.Raw)
		if !strings.HasPrefix(raw, "Subject: =?UTF-8?Q?") {
			t.Errorf("%q : objet non encodé RFC 2047 : %q", s, raw)
		}
		for _, l := range strings.Split(raw, "\r\n") {
			if len(l) > 78 {
				t.Errorf("%q : ligne d'objet de %d caractères", s, len(l))
			}
			for _, w := range strings.Fields(strings.TrimPrefix(l, "Subject:")) {
				if len(w) > 75 || !strings.HasPrefix(w, "=?UTF-8?Q?") || !strings.HasSuffix(w, "?=") {
					t.Errorf("%q : mot encodé invalide %q", s, w)
				}
			}
		}
		got, err := dec.DecodeHeader(parse(t, out.Raw).Header.Get("Subject"))
		if err != nil || got != s {
			t.Errorf("objet décodé %q (%v), attendu %q", got, err, s)
		}
	}
}

func TestSubjectLengthLimit(t *testing.T) {
	sixty := strings.Repeat("é", 59) + "x"
	if err := LintSubject(sixty); err != nil {
		t.Errorf("objet de 60 caractères refusé : %v", err)
	}
	assertKind(t, LintSubject(sixty+"y"), ErrSubject)

	b := newBuilder(t, nil)
	m := validMessage()
	m.Subject = sixty
	build(t, b, m)
	m.Subject = sixty + "y"
	_, err := b.Build(m)
	assertKind(t, err, ErrSubject)
}

func TestSubjectRefusesShouting(t *testing.T) {
	for _, s := range []string{
		"Tirage TERMINÉ",
		"URGENT : ton tirage",
		"Ton cadeau GRATUIT",
		"Noël NOËL",
		"Vite !!!",
		"Ouvre-moi!!!!",
		"",
		"   ",
	} {
		assertKind(t, LintSubject(s), ErrSubject)
		m := validMessage()
		m.Subject = s
		_, err := newBuilder(t, nil).Build(m)
		assertKind(t, err, ErrSubject)
	}
	for _, s := range []string{
		"J-3 avant Noël",
		"À toi de jouer",
		"Ton tirage est prêt !",
		"Échange 2026 : rappel",
		"Vite !!",
	} {
		if err := LintSubject(s); err != nil {
			t.Errorf("%q refusé : %v", s, err)
		}
	}
}
