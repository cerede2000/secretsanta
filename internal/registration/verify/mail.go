package verify

import (
	"context"
	"errors"

	"secretsanta/internal/i18n/catalog"
	"secretsanta/internal/mail/queue"
	"secretsanta/internal/mail/templates"
)

// WelcomeMailType est la nature du courriel « bienvenue » dans la file.
const WelcomeMailType = "bienvenue"

// Welcome est le contenu du courriel « bienvenue ». Il porte le lien
// personnel en clair : il n'existe que le temps de l'envoi.
type Welcome struct {
	GroupID       int64
	ParticipantID int64
	To            string
	FirstName     string
	// Lang est la langue de la personne (peut être vide) ; GroupLang celle
	// du groupe.
	Lang      string
	GroupLang string
	GroupName string
	// Link est le lien personnel ; Code le code personnel de F2b.
	Link string
	Code string
}

// WelcomeMailer envoie le courriel « bienvenue ».
type WelcomeMailer interface {
	SendWelcome(ctx context.Context, w Welcome) error
}

// WelcomeMailerFunc adapte une fonction en WelcomeMailer.
type WelcomeMailerFunc func(ctx context.Context, w Welcome) error

// SendWelcome implémente WelcomeMailer.
func (f WelcomeMailerFunc) SendWelcome(ctx context.Context, w Welcome) error { return f(ctx, w) }

// Enqueuer met un courriel en file ; *queue.Service le satisfait.
type Enqueuer interface {
	EnqueueMail(ctx context.Context, it queue.Item) (int64, error)
}

// TemplateMailer compose le courriel avec le modèle « bienvenue » de F6
// (templates.Welcome : lien personnel et incitation à écrire sa lettre) et
// le met dans la file d'envoi.
type TemplateMailer struct {
	Catalog *catalog.Catalog
	Queue   Enqueuer
}

var _ WelcomeMailer = TemplateMailer{}

// SendWelcome implémente WelcomeMailer.
func (t TemplateMailer) SendWelcome(ctx context.Context, w Welcome) error {
	if t.Catalog == nil || t.Queue == nil {
		return errors.New("verify: catalogue ou file d'envoi absent")
	}
	msg, err := templates.Render(t.Catalog, templates.Welcome, templates.Data{
		To: w.To, FirstName: w.FirstName,
		ParticipantLang: w.Lang, GroupLang: w.GroupLang, GroupName: w.GroupName,
		Link: w.Link, Code: w.Code,
	})
	if err != nil {
		return err
	}
	_, err = t.Queue.EnqueueMail(ctx, queue.Item{
		GroupID: w.GroupID, ParticipantID: w.ParticipantID, To: msg.To, ToName: msg.ToName,
		Type: WelcomeMailType, Essential: true,
		Subject: msg.Subject, Text: msg.Text, HTML: msg.HTML,
	})
	return err
}
