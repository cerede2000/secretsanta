package mailunsub

import (
	"bytes"
	"net/mail"
	"strings"
	"testing"

	"secretsanta/internal/mail/templates"
)

// F6.AC16 : chaque mail non essentiel porte un lien signé HMAC dans le corps
// (texte et HTML) et les en-têtes List-Unsubscribe et List-Unsubscribe-Post.
func TestNonEssentialMailsCarrySignedLinkAndHeaders(t *testing.T) {
	e := newEnv(t)
	want := map[templates.Kind]string{
		templates.ReminderParticipant: TypeRelances,
		templates.ReminderLetter:      TypeRelances,
		templates.ReminderReading:     TypeRelances,
		templates.LetterUpdated:       TypeNouvelleIdee,
	}
	n := 0
	for _, k := range templates.Kinds() {
		info, _ := templates.InfoOf(k)
		if info.Essential {
			continue
		}
		n++
		typ, ok := want[k]
		if !ok {
			t.Fatalf("mail non essentiel %s absent du test", k)
		}
		m, raw := render(t, e.cat, e.svc, k, sample(k))
		if !strings.HasPrefix(m.UnsubscribeURL, publicURL+RoutePrefix) {
			t.Fatalf("%s : lien %q", k, m.UnsubscribeURL)
		}
		if !strings.Contains(m.Text, m.UnsubscribeURL) || !strings.Contains(m.HTML, m.UnsubscribeURL) {
			t.Errorf("%s : lien absent du corps texte ou HTML", k)
		}
		c, err := e.svc.Signer().Verify(strings.TrimPrefix(m.UnsubscribeURL, publicURL+RoutePrefix))
		if err != nil || c.Type != typ || c.ParticipantID != pid || c.GroupID != 1 {
			t.Errorf("%s : jeton %+v (%v)", k, c, err)
		}
		msg, err := mail.ReadMessage(bytes.NewReader([]byte(raw)))
		if err != nil {
			t.Fatal(err)
		}
		if got := msg.Header.Get("List-Unsubscribe"); !strings.Contains(got, "<"+m.UnsubscribeURL+">") || !strings.HasPrefix(m.UnsubscribeURL, "https://") {
			t.Errorf("%s : List-Unsubscribe = %q", k, got)
		}
		if got := msg.Header.Get("List-Unsubscribe-Post"); got != "List-Unsubscribe=One-Click" {
			t.Errorf("%s : List-Unsubscribe-Post = %q", k, got)
		}
	}
	if n != len(want) {
		t.Fatalf("modèles non essentiels : %d, attendus %d", n, len(want))
	}
}

func TestSignatureCoversParticipantGroupAndType(t *testing.T) {
	s := NewSigner([]byte("k"))
	a, _ := s.Sign(Claims{GroupID: 1, ParticipantID: 2, Type: TypeRelances})
	b, _ := s.Sign(Claims{GroupID: 1, ParticipantID: 3, Type: TypeRelances})
	c, _ := s.Sign(Claims{GroupID: 2, ParticipantID: 2, Type: TypeRelances})
	d, _ := s.Sign(Claims{GroupID: 1, ParticipantID: 2, Type: TypeNouvelleIdee})
	if a == b || a == c || a == d {
		t.Fatal("la signature ne couvre pas tous les champs")
	}
	if _, err := NewSigner([]byte("autre")).Verify(a); err == nil {
		t.Fatal("un autre secret ne doit pas valider")
	}
	if _, err := s.Sign(Claims{GroupID: 1, ParticipantID: 2, Type: "magic_link"}); err == nil {
		t.Fatal("un type essentiel ne se signe pas")
	}
}
