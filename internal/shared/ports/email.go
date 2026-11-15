// Package ports définit les interfaces partagées entre fonctionnalités.
package ports

import "context"

// EmailType identifie la nature d'un courriel.
type EmailType string

// EmailMessage est un courriel à envoyer.
type EmailMessage struct {
	To      string
	Subject string
	Body    string
	// Type est la nature du courriel (invitation, rappel, ...).
	Type EmailType
	// Essential indique un courriel qu'aucune préférence ne peut couper.
	Essential bool
	// GroupingKey regroupe des courriels en un seul envoi (facultatif).
	GroupingKey string
}

// EmailSender est le port d'envoi de courriel (implémenté par F6).
type EmailSender interface {
	Send(ctx context.Context, msg EmailMessage) error
}

// FakeEmailSender est une doublure qui mémorise les messages envoyés.
type FakeEmailSender struct {
	Sent []EmailMessage
	Err  error
}

// Send implémente EmailSender.
func (f *FakeEmailSender) Send(_ context.Context, msg EmailMessage) error {
	if f.Err != nil {
		return f.Err
	}
	f.Sent = append(f.Sent, msg)
	return nil
}
