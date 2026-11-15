// Package reminders envoie les relances (F7.2 à F7.4) : planificateur
// automatique (lien non ouvert, lettre non écrite depuis joined_at), relance
// manuelle d'une personne ou d'une liste, liste de remise manuelle des
// personnes sans courriel. Les messages passent par la file de F6 avec les
// modèles de rappel ; aucun ne nomme une autre personne que le destinataire.
package reminders

import (
	"context"
	"database/sql"
	"log/slog"
	"strings"
	"time"

	"secretsanta/internal/i18n/catalog"
	"secretsanta/internal/mail/queue"
	"secretsanta/internal/mail/templates"
	"secretsanta/internal/mailunsub"
	"secretsanta/internal/participants"
	"secretsanta/internal/reminderrules"
	"secretsanta/internal/shared/clock"
	"secretsanta/internal/shared/errs"
	"secretsanta/internal/shared/ports"
)

// Kind est le type d'une relance.
type Kind string

// Les types de relance. « lecture » (relance du tireur qui n'a pas ouvert sa
// cible) n'existe qu'en manuel ici ; son automatisme est livré par F7b.
const (
	KindLink   Kind = "lien"
	KindLetter Kind = "lettre"
	KindRead   Kind = "lecture"
)

func (k Kind) template() templates.Kind {
	switch k {
	case KindLetter:
		return templates.ReminderLetter
	case KindRead:
		return templates.ReminderReading
	}
	return templates.ReminderParticipant
}

// GroupInfo décrit le groupe d'une relance.
type GroupInfo struct {
	ID   int64
	Slug string
	Name string
	Lang string // langue par défaut du groupe, vide si non définie
}

// Directory donne les groupes et les participants.
type Directory interface {
	GroupIDs(ctx context.Context) ([]int64, error)
	Group(ctx context.Context, groupID int64) (GroupInfo, error)
	Participants(ctx context.Context, groupID int64) ([]participants.Participant, error)
}

// Queue est la file de courriels de F6.
type Queue interface {
	EnqueueMail(ctx context.Context, it queue.Item) (int64, error)
}

// Unsubscriber est la désinscription par type de F6 (*mailunsub.Service).
type Unsubscriber interface {
	ShouldSend(ctx context.Context, kind templates.Kind, participantID int64) (bool, error)
	Decorate(kind templates.Kind, groupID, participantID int64, d templates.Data) (templates.Data, error)
}

// Options rassemble les dépendances du moteur.
type Options struct {
	DB        *sql.DB
	Clock     clock.Clock
	Rules     *reminderrules.Service
	Directory Directory
	Facts     Facts
	Queue     Queue
	Unsub     Unsubscriber
	Suppress  ports.SuppressionList
	Catalog   *catalog.Catalog
	// LinkFor donne le lien mis dans la relance ; par défaut la page de
	// connexion du groupe sous PublicURL.
	LinkFor   func(g GroupInfo, p participants.Participant) string
	PublicURL string
	Logger    *slog.Logger
}

// Engine est le moteur de relances.
type Engine struct{ o Options }

// New crée le moteur.
func New(o Options) *Engine {
	if o.Logger == nil {
		o.Logger = slog.Default()
	}
	if o.LinkFor == nil {
		base := strings.TrimRight(o.PublicURL, "/")
		o.LinkFor = func(g GroupInfo, _ participants.Participant) string { return base + "/" + g.Slug + "/connexion" }
	}
	return &Engine{o: o}
}

// Outcome est le sort d'une relance pour une personne.
type Outcome string

// Les issues possibles (résultat de la relance manuelle).
const (
	OutcomeQueued         Outcome = "mise_en_file"
	OutcomeAlreadyToday   Outcome = "deja_relancee_aujourdhui"
	OutcomeUnsubscribed   Outcome = "desinscrite"
	OutcomeAddressRefused Outcome = "adresse_refusee"
	OutcomeNoEmail        Outcome = "sans_courriel"
	OutcomeNotNeeded      Outcome = "inutile"
	OutcomeUnknown        Outcome = "inconnue"
)

// MayNotify dit si un courriel de ce modèle peut partir à la personne : une
// désinscription du type (relances, nouvelle_idee) le coupe, jamais un mail
// essentiel. F7b l'appelle avant la notification « nouvelle idée ».
func (e *Engine) MayNotify(ctx context.Context, kind templates.Kind, participantID int64) (bool, error) {
	return e.o.Unsub.ShouldSend(ctx, kind, participantID)
}

// send tente d'envoyer une relance : contrôles dans l'ordre, puis réservation
// du courriel du jour, mise en file et journal.
func (e *Engine) send(ctx context.Context, g GroupInfo, p participants.Participant, kind Kind, auto bool) (Outcome, error) {
	if kind == KindRead && p.Role == participants.RoleReceptionSeule {
		return OutcomeNotNeeded, nil // aucune cible : jamais de relance de révélation
	}
	if strings.TrimSpace(p.Email) == "" {
		return OutcomeNoEmail, nil
	}
	sup, err := e.o.Suppress.IsSuppressed(ctx, g.ID, p.Email)
	if err != nil {
		return "", errs.Internal(err)
	}
	if sup {
		return OutcomeAddressRefused, nil
	}
	tk := kind.template()
	ok, err := e.o.Unsub.ShouldSend(ctx, tk, p.ID)
	if err != nil {
		return "", errs.Internal(err)
	}
	if !ok {
		return OutcomeUnsubscribed, nil
	}
	d, err := e.o.Unsub.Decorate(tk, g.ID, p.ID, templates.Data{
		To: p.Email, FirstName: p.FirstName, GroupName: g.Name,
		ParticipantLang: p.Locale, GroupLang: g.Lang, Link: e.o.LinkFor(g, p),
	})
	if err != nil {
		return "", errs.Internal(err)
	}
	msg, err := templates.Render(e.o.Catalog, tk, d)
	if err != nil {
		return "", errs.Internal(err)
	}
	claimed, err := e.o.Rules.Claim(ctx, g.ID, p.ID, string(kind))
	if err != nil {
		return "", err
	}
	if !claimed {
		return OutcomeAlreadyToday, nil
	}
	_, err = e.o.Queue.EnqueueMail(ctx, queue.Item{
		GroupID: g.ID, ParticipantID: p.ID, To: p.Email, ToName: p.FirstName,
		Type: mailunsub.TypeRelances, Subject: msg.Subject, Text: msg.Text, HTML: msg.HTML, Lang: msg.Lang,
		UnsubscribeURL: d.UnsubscribeURL,
	})
	if err != nil {
		_ = e.o.Rules.Release(ctx, g.ID, p.ID)
		return "", err
	}
	a := 0
	if auto {
		a = 1
	}
	if _, err := e.o.DB.ExecContext(ctx, `INSERT INTO reminders_sent (group_id, participant_id, kind, automatic, sent_at)
		VALUES (?, ?, ?, ?, ?)`, g.ID, p.ID, string(kind), a, e.o.Clock.Now().UTC().Format(time.RFC3339)); err != nil {
		return "", errs.Internal(err)
	}
	return OutcomeQueued, nil
}

// late dit si la personne est en retard pour ce type de relance.
func (e *Engine) late(ctx context.Context, groupID int64, p participants.Participant, kind Kind, unread map[int64]bool) (bool, error) {
	switch kind {
	case KindLink:
		done, err := e.o.Facts.LinkOpened(ctx, groupID, p.ID)
		return !done, err
	case KindLetter:
		done, err := e.o.Facts.LetterWritten(ctx, groupID, p.ID)
		return !done, err
	case KindRead:
		return unread[p.ID] && p.Role != participants.RoleReceptionSeule, nil
	}
	return false, nil
}
