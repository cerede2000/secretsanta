package code

import (
	"context"
	"strings"
	"testing"

	"secretsanta/internal/i18n/catalog"
	"secretsanta/internal/mail/queue"
	"secretsanta/web"
)

type captureQueue struct{ items []queue.Item }

func (c *captureQueue) EnqueueMail(_ context.Context, it queue.Item) (int64, error) {
	c.items = append(c.items, it)
	return int64(len(c.items)), nil
}

// Le courriel part avec le modèle « code de vérification » de F6 : code,
// lien de confirmation en un clic, essentiel.
func TestTemplateMailerUsesVerificationTemplate(t *testing.T) {
	cat, err := catalog.Load(web.I18n, catalog.Options{DefaultLang: "fr"})
	if err != nil {
		t.Fatal(err)
	}
	q := &captureQueue{}
	f := newFixture(t)
	svc, err := New(Options{Store: f.store, Clock: f.clk, Mailer: TemplateMailer{Catalog: cat, Queue: q}, Link: testLink, Key: testKey})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Issue(context.Background(), request(testGroup, 1, "lea@example.org")); err != nil {
		t.Fatal(err)
	}
	if len(q.items) != 1 {
		t.Fatalf("%d courriels en file", len(q.items))
	}
	it := q.items[0]
	if it.To != "lea@example.org" || it.GroupID != testGroup || !it.Essential || it.Type != MailType || it.UnsubscribeURL != "" {
		t.Fatalf("courriel %+v", it)
	}
	link := "https://noel.example.org/famille7/inscription/confirmer/"
	for _, part := range []string{it.Text, it.HTML} {
		if !strings.Contains(part, "Bonjour Léa,") || !strings.Contains(part, link) || !strings.Contains(part, "15 minutes") {
			t.Errorf("contenu incomplet :\n%s", part)
		}
	}
	// Le code présent dans le courriel est bien celui du lien.
	i := strings.Index(it.Text, link)
	tok := strings.Fields(it.Text[i+len(link):])[0]
	p, err := svc.Preview(context.Background(), testGroup, tok)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(it.Text, p.Code) || !strings.Contains(it.HTML, p.Code) {
		t.Fatalf("code %q absent du courriel", p.Code)
	}
}
