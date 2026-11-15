package code

import (
	"context"
	"errors"

	"secretsanta/internal/i18n/catalog"
	"secretsanta/internal/mail/queue"
	"secretsanta/internal/mail/templates"
)

// Enqueuer met un courriel en file ; *queue.Service le satisfait.
type Enqueuer interface {
	EnqueueMail(ctx context.Context, it queue.Item) (int64, error)
}

// TemplateMailer compose le courriel avec le modèle « code de vérification »
// de F6 (templates.Verification) et le met dans la file d'envoi.
type TemplateMailer struct {
	Catalog *catalog.Catalog
	Queue   Enqueuer
}

var _ Mailer = TemplateMailer{}

// SendVerification implémente Mailer.
func (t TemplateMailer) SendVerification(ctx context.Context, m Mail) error {
	if t.Catalog == nil || t.Queue == nil {
		return errors.New("code: catalogue ou file d'envoi absent")
	}
	msg, err := templates.Render(t.Catalog, templates.Verification, templates.Data{
		To: m.To, FirstName: m.FirstName,
		ParticipantLang: m.Lang, GroupLang: m.GroupLang, GroupName: m.GroupName,
		Link: m.Link, Code: m.Code,
	})
	if err != nil {
		return err
	}
	_, err = t.Queue.EnqueueMail(ctx, queue.Item{
		GroupID: m.GroupID, To: msg.To, ToName: msg.ToName,
		Type: MailType, Essential: true,
		Subject: msg.Subject, Text: msg.Text, HTML: msg.HTML, Lang: msg.Lang,
	})
	return err
}
